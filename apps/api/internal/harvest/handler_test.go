package harvest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
)

type fakeExecutor struct {
	calls []HarvestCommand
	out   HarvestOutcome
	err   error
}

func (f *fakeExecutor) Harvest(_ context.Context, cmd HarvestCommand) (HarvestOutcome, error) {
	f.calls = append(f.calls, cmd)
	return f.out, f.err
}

func TestJobHandler_ExecutesPayload(t *testing.T) {
	exec := &fakeExecutor{out: HarvestOutcome{NetYield: "9", TxHash: "abc"}}
	h := NewJobHandler(exec, nil)

	vid, uid := uuid.New(), uuid.New()
	payload, _ := json.Marshal(HarvestJobPayload{VaultID: vid, UserID: uid, WalletAddress: "GABC"})

	if err := h.Handle(context.Background(), jobqueue.Job{Type: "harvest", Payload: payload}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(exec.calls) != 1 {
		t.Fatalf("executor called %d times, want 1", len(exec.calls))
	}
	if exec.calls[0].Payload.VaultID != vid || exec.calls[0].Payload.WalletAddress != "GABC" {
		t.Fatalf("payload not forwarded correctly: %+v", exec.calls[0].Payload)
	}
}

func TestJobHandler_MalformedPayloadIsPermanent(t *testing.T) {
	h := NewJobHandler(&fakeExecutor{}, nil)
	err := h.Handle(context.Background(), jobqueue.Job{Type: "harvest", Payload: []byte(`{not json`)})
	if err == nil || !jobqueue.IsPermanent(err) {
		t.Fatalf("malformed payload should be a permanent failure, got %v", err)
	}
}

func TestJobHandler_ExecutorErrorRetries(t *testing.T) {
	exec := &fakeExecutor{err: errors.New("rpc timeout")}
	h := NewJobHandler(exec, nil)

	payload, _ := json.Marshal(HarvestJobPayload{VaultID: uuid.New(), UserID: uuid.New()})
	err := h.Handle(context.Background(), jobqueue.Job{Type: "harvest", Payload: payload})
	if err == nil {
		t.Fatal("expected a transient error to propagate for retry")
	}
	if jobqueue.IsPermanent(err) {
		t.Fatal("transient executor error must not be permanent")
	}
}
