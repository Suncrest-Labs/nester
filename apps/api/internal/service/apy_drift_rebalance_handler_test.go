package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
)

type fakeRebalanceTrigger struct {
	resp admindomain.RebalanceResponse
	err  error
	// calls records the vault IDs and requests TriggerRebalance was invoked
	// with, so tests can assert both that it ran and with what.
	calls []struct {
		vaultID uuid.UUID
		req     admindomain.RebalanceRequest
	}
}

func (f *fakeRebalanceTrigger) TriggerRebalance(_ context.Context, vaultID uuid.UUID, req admindomain.RebalanceRequest) (admindomain.RebalanceResponse, error) {
	f.calls = append(f.calls, struct {
		vaultID uuid.UUID
		req     admindomain.RebalanceRequest
	}{vaultID, req})
	if f.err != nil {
		return admindomain.RebalanceResponse{}, f.err
	}
	return f.resp, nil
}

type fakeAuditLogger struct {
	entries []AuditEntry
	err     error
}

func (f *fakeAuditLogger) Log(_ context.Context, entry AuditEntry) error {
	f.entries = append(f.entries, entry)
	return f.err
}

func testDriftJob(t *testing.T, payload RebalanceDriftJobPayload) jobqueue.Job {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return jobqueue.Job{ID: uuid.New(), Type: RebalanceDriftJobType, Payload: raw}
}

func TestAPYDriftRebalanceHandler_SubmitsAndAudits(t *testing.T) {
	vaultID := uuid.New()
	rebalanceID := uuid.New()
	admin := &fakeRebalanceTrigger{resp: admindomain.RebalanceResponse{
		Status:      admindomain.RebalanceStatusSubmitted,
		TxHash:      "tx123",
		RebalanceID: rebalanceID,
	}}
	audit := &fakeAuditLogger{}
	h := NewAPYDriftRebalanceJobHandler(admin, audit, nil)

	job := testDriftJob(t, RebalanceDriftJobPayload{
		VaultID:            vaultID,
		CurrentTopProtocol: "protocol-a",
		OptimalProtocol:    "protocol-b",
		DriftBPS:           900,
		ThresholdBPS:       200,
		DetectedAt:         time.Now().UTC(),
	})

	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(admin.calls) != 1 {
		t.Fatalf("expected exactly 1 TriggerRebalance call, got %d", len(admin.calls))
	}
	call := admin.calls[0]
	if call.vaultID != vaultID {
		t.Errorf("expected vault %s, got %s", vaultID, call.vaultID)
	}
	if call.req.Strategy != admindomain.RebalanceStrategyAuto || call.req.DryRun {
		t.Errorf("expected a live auto-strategy request, got %+v", call.req)
	}

	if len(audit.entries) != 1 {
		t.Fatalf("expected exactly 1 audit entry, got %d", len(audit.entries))
	}
	if audit.entries[0].Action != "vault.rebalance.apy_drift_triggered" {
		t.Errorf("unexpected audit action: %q", audit.entries[0].Action)
	}
	if audit.entries[0].EntityID != vaultID {
		t.Errorf("expected audit entry entity id %s, got %s", vaultID, audit.entries[0].EntityID)
	}
}

// TestAPYDriftRebalanceHandler_AuditEntryCarriesJobCorrelationID covers
// nester#1339: the audit entry this handler writes on successful submission
// previously had no way to carry the id of the job run that produced it.
// It must now reuse the enqueued job's own CorrelationID (set at enqueue
// time by APYDriftDetector.evaluateAndEnqueue) rather than minting its own
// or leaving it empty.
func TestAPYDriftRebalanceHandler_AuditEntryCarriesJobCorrelationID(t *testing.T) {
	vaultID := uuid.New()
	admin := &fakeRebalanceTrigger{resp: admindomain.RebalanceResponse{
		Status: admindomain.RebalanceStatusSubmitted,
	}}
	audit := &fakeAuditLogger{}
	h := NewAPYDriftRebalanceJobHandler(admin, audit, nil)

	job := testDriftJob(t, RebalanceDriftJobPayload{
		VaultID:            vaultID,
		CurrentTopProtocol: "protocol-a",
		OptimalProtocol:    "protocol-b",
		DriftBPS:           900,
		ThresholdBPS:       200,
		DetectedAt:         time.Now().UTC(),
	})
	job.CorrelationID = "corr-abc-123"

	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if len(audit.entries) != 1 {
		t.Fatalf("expected exactly 1 audit entry, got %d", len(audit.entries))
	}
	if audit.entries[0].CorrelationID != "corr-abc-123" {
		t.Errorf("audit entry CorrelationID = %q, want %q", audit.entries[0].CorrelationID, "corr-abc-123")
	}
}

func TestAPYDriftRebalanceHandler_TreatsInFlightAsNonRetryableNoOp(t *testing.T) {
	admin := &fakeRebalanceTrigger{err: ErrRebalanceInFlight}
	audit := &fakeAuditLogger{}
	h := NewAPYDriftRebalanceJobHandler(admin, audit, nil)

	job := testDriftJob(t, RebalanceDriftJobPayload{VaultID: uuid.New()})

	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("expected ErrRebalanceInFlight to be treated as a no-op, got error: %v", err)
	}
	if len(audit.entries) != 0 {
		t.Errorf("expected no audit entry for a no-op outcome, got %d", len(audit.entries))
	}
}

func TestAPYDriftRebalanceHandler_TreatsNotEligibleAsNonRetryableNoOp(t *testing.T) {
	admin := &fakeRebalanceTrigger{err: ErrRebalanceNotEligible}
	h := NewAPYDriftRebalanceJobHandler(admin, nil, nil)

	job := testDriftJob(t, RebalanceDriftJobPayload{VaultID: uuid.New()})

	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("expected ErrRebalanceNotEligible to be treated as a no-op, got error: %v", err)
	}
}

func TestAPYDriftRebalanceHandler_RetriesOnUnexpectedError(t *testing.T) {
	admin := &fakeRebalanceTrigger{err: assertErr("db connection reset")}
	h := NewAPYDriftRebalanceJobHandler(admin, nil, nil)

	job := testDriftJob(t, RebalanceDriftJobPayload{VaultID: uuid.New()})

	err := h.Handle(context.Background(), job)
	if err == nil {
		t.Fatal("expected an unexpected TriggerRebalance error to propagate for retry")
	}
	if jobqueue.IsPermanent(err) {
		t.Error("expected a retryable error, not a permanent one, for an unrecognized failure")
	}
}

func TestAPYDriftRebalanceHandler_RejectsPayloadWithNoVaultID(t *testing.T) {
	admin := &fakeRebalanceTrigger{}
	h := NewAPYDriftRebalanceJobHandler(admin, nil, nil)

	job := testDriftJob(t, RebalanceDriftJobPayload{}) // zero VaultID

	err := h.Handle(context.Background(), job)
	if err == nil {
		t.Fatal("expected an error for a payload with no vault id")
	}
	if !jobqueue.IsPermanent(err) {
		t.Error("expected a permanent (non-retryable) error for a malformed payload")
	}
	if len(admin.calls) != 0 {
		t.Error("expected TriggerRebalance not to be called for an invalid payload")
	}
}

func TestAPYDriftRebalanceHandler_RejectsMalformedPayload(t *testing.T) {
	admin := &fakeRebalanceTrigger{}
	h := NewAPYDriftRebalanceJobHandler(admin, nil, nil)

	job := jobqueue.Job{ID: uuid.New(), Type: RebalanceDriftJobType, Payload: []byte("not json")}

	err := h.Handle(context.Background(), job)
	if err == nil {
		t.Fatal("expected an error for a malformed payload")
	}
	if !jobqueue.IsPermanent(err) {
		t.Error("expected a permanent (non-retryable) error for malformed JSON")
	}
}

func TestAPYDriftRebalanceHandler_AuditFailureDoesNotFailTheJob(t *testing.T) {
	// The rebalance already succeeded on-chain by the time the audit write
	// runs; failing (and therefore retrying) the job here would risk a
	// second on-chain submission for a purely observability-side failure.
	admin := &fakeRebalanceTrigger{resp: admindomain.RebalanceResponse{
		Status:      admindomain.RebalanceStatusSubmitted,
		RebalanceID: uuid.New(),
	}}
	audit := &fakeAuditLogger{err: assertErr("audit table unavailable")}
	h := NewAPYDriftRebalanceJobHandler(admin, audit, nil)

	job := testDriftJob(t, RebalanceDriftJobPayload{VaultID: uuid.New()})

	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("expected the job to succeed despite an audit-log failure, got: %v", err)
	}
}
