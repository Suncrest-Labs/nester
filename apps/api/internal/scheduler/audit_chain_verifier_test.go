package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stubChainVerifier satisfies the narrow ChainVerifier interface.
type stubChainVerifier struct {
	ok          bool
	brokenSeq   int64
	verifyErr   error
	verifyCalls int
}

func (s *stubChainVerifier) VerifyChain(_ context.Context, _, _ int64) (bool, int64, error) {
	s.verifyCalls++
	return s.ok, s.brokenSeq, s.verifyErr
}

// testAlerter is a simple call-recording spy.
type testAlerter struct {
	calls     int
	lastSeq   int64
	lastError error
}

func (a *testAlerter) AlertChainBreak(_ context.Context, seq int64, err error) {
	a.calls++
	a.lastSeq = seq
	a.lastError = err
}

func TestAuditChainVerifier_NoAlertOnCleanChain(t *testing.T) {
	svc := &stubChainVerifier{ok: true}
	alerter := &testAlerter{}
	cfg := AuditChainVerifierConfig{Enabled: true, Interval: time.Hour}

	v := NewAuditChainVerifier(cfg, svc, alerter, nil)
	v.runOnce(context.Background())

	if alerter.calls != 0 {
		t.Errorf("expected 0 alert calls for clean chain, got %d", alerter.calls)
	}
	if svc.verifyCalls != 1 {
		t.Errorf("expected 1 verify call, got %d", svc.verifyCalls)
	}
}

func TestAuditChainVerifier_AlertOnBrokenChain(t *testing.T) {
	brokenErr := errors.New("prev_hash mismatch at sequence 3")
	alerter := &testAlerter{}
	svc := &stubChainVerifier{ok: false, brokenSeq: 3, verifyErr: brokenErr}
	cfg := AuditChainVerifierConfig{Enabled: true, Interval: time.Hour}

	v := NewAuditChainVerifier(cfg, svc, alerter, nil)
	v.runOnce(context.Background())

	if alerter.calls != 1 {
		t.Errorf("expected 1 alert call on break, got %d", alerter.calls)
	}
	if alerter.lastSeq != 3 {
		t.Errorf("expected break at seq 3, got %d", alerter.lastSeq)
	}
}

func TestAuditChainVerifier_DisabledSkipsVerification(t *testing.T) {
	alerter := &testAlerter{}
	svc := &stubChainVerifier{ok: true}

	cfg := AuditChainVerifierConfig{Enabled: false, Interval: 1 * time.Millisecond}
	v := NewAuditChainVerifier(cfg, svc, alerter, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	v.Run(ctx)

	if svc.verifyCalls != 0 {
		t.Errorf("expected 0 verify calls when disabled, got %d", svc.verifyCalls)
	}
	if alerter.calls != 0 {
		t.Errorf("expected 0 alert calls when disabled, got %d", alerter.calls)
	}
}

func TestAuditChainVerifier_AlerCalledDirectly(t *testing.T) {
	alerter := &testAlerter{}
	testErr := errors.New("entry_hash mismatch")

	alerter.AlertChainBreak(context.Background(), 7, testErr)

	if alerter.calls != 1 {
		t.Errorf("expected 1 call, got %d", alerter.calls)
	}
	if alerter.lastSeq != 7 {
		t.Errorf("expected seq 7, got %d", alerter.lastSeq)
	}
	if !errors.Is(alerter.lastError, testErr) {
		t.Errorf("expected err %v, got %v", testErr, alerter.lastError)
	}
}
