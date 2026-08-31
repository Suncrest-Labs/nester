package signing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Game-day rehearsal harness.
//
// Scenario: an attacker has compromised the API process and attempts to abuse
// its signing capability.
//
// This is an executable rehearsal, not a description of one. Each phase below
// is a real assertion against the real enforcement code, so the runbook's claims
// are checked by CI on every run rather than believed. What it deliberately does
// NOT do is touch production, real funds, or a real network — the backend is a
// stub, so no transaction is ever built or broadcast.
//
// What this harness can and cannot exercise is recorded honestly in
// docs/security/game-day.md. In particular it cannot rehearse the on-chain
// operator authority transfer (§D.1 step 3 of the runbook), because that
// requires a funded testnet account and the deployed contracts.

// gameDayRig is the rehearsal environment: a signer with a policy, a kill
// switch, a recording audit sink, and an HTTP surface, standing in for the
// deployed signer.
type gameDayRig struct {
	t        *testing.T
	backend  *stubBackend
	sink     *recordingSink
	service  *Service
	server   *httptest.Server
	sentinel string
	// caller is swapped to simulate an unauthorized caller.
	callerErr error
}

func newGameDayRig(t *testing.T) *gameDayRig {
	t.Helper()

	backend := &stubBackend{keyID: "GOPERATORKEYFORREHEARSALONLY"}
	sink := &recordingSink{}
	ks, sentinel := newTestKillSwitch(t)

	svc, err := NewService(ServiceOptions{
		Backend:    backend,
		Policy:     testPolicy(),
		KillSwitch: ks,
		Sink:       sink,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("game day: build signer: %v", err)
	}

	rig := &gameDayRig{
		t:        t,
		backend:  backend,
		sink:     sink,
		service:  svc,
		sentinel: sentinel,
	}
	srv := NewServer(svc, func(*http.Request) (string, error) {
		if rig.callerErr != nil {
			return "", rig.callerErr
		}
		return "nester-api", nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	rig.server = httptest.NewServer(srv.Handler())
	t.Cleanup(rig.server.Close)
	return rig
}

// attemptSign performs a signing request the way the compromised API would.
func (r *gameDayRig) attemptSign(intent *Intent) (int, wireError) {
	r.t.Helper()
	body, err := json.Marshal(wireRequest{Intent: intent})
	if err != nil {
		r.t.Fatalf("marshal intent: %v", err)
	}
	resp, err := http.Post(r.server.URL+SignPath, "application/json", strings.NewReader(string(body)))
	if err != nil {
		r.t.Fatalf("sign request: %v", err)
	}
	defer resp.Body.Close()

	var werr wireError
	_ = json.NewDecoder(resp.Body).Decode(&werr)
	return resp.StatusCode, werr
}

func (r *gameDayRig) engageKillSwitch(reason string) {
	r.t.Helper()
	if err := os.WriteFile(r.sentinel, []byte(reason), 0o600); err != nil {
		r.t.Fatalf("engage kill switch: %v", err)
	}
}

func (r *gameDayRig) releaseKillSwitch() {
	r.t.Helper()
	if err := os.Remove(r.sentinel); err != nil && !os.IsNotExist(err) {
		r.t.Fatalf("release kill switch: %v", err)
	}
}

// TestGameDay_CompromisedAPIAbusingSigning walks the full rehearsal in order,
// mirroring the phases of docs/security/incident-response.md.
func TestGameDay_CompromisedAPIAbusingSigning(t *testing.T) {
	rig := newGameDayRig(t)
	ctx := context.Background()
	_ = ctx

	// ── Phase 0: establish the baseline ──────────────────────────────────
	// Normal operation. The rehearsal needs a "before" to compare against.
	t.Run("phase0_baseline_legitimate_signing_works", func(t *testing.T) {
		status, werr := rig.attemptSign(validDepositIntent(time.Now().UTC()))
		if status != http.StatusOK {
			t.Fatalf("legitimate signing failed at baseline: status=%d err=%v", status, werr)
		}
		if rig.backend.callCount() != 1 {
			t.Fatalf("expected 1 signature at baseline, got %d", rig.backend.callCount())
		}
	})

	// ── Phase 1: the attack ──────────────────────────────────────────────
	// The attacker controls the API and tries the transactions that would
	// actually extract value. Each must be refused, and each refusal must be
	// recorded — the recording is what makes the incident investigable.
	t.Run("phase1_attack_attempts_are_refused_and_recorded", func(t *testing.T) {
		signaturesBefore := rig.backend.callCount()

		attacks := []struct {
			name   string
			intent *Intent
			expect Rejection
		}{
			{
				name: "drain via an unmodelled contract function",
				intent: func() *Intent {
					i := validDepositIntent(time.Now().UTC())
					i.ID = "atk-1"
					i.Operation = "transfer_all"
					return i
				}(),
				expect: RejectUnknownOperation,
			},
			{
				name: "redirect funds to an attacker-controlled contract",
				intent: func() *Intent {
					i := validDepositIntent(time.Now().UTC())
					i.ID = "atk-2"
					i.ContractAddress = otherContract
					return i
				}(),
				expect: RejectContractNotAllowed,
			},
			{
				name: "extract more than the per-transaction limit",
				intent: func() *Intent {
					i := validDepositIntent(time.Now().UTC())
					i.ID = "atk-3"
					i.Arg0 = 999_999_999_999
					return i
				}(),
				expect: RejectAmountOutOfPolicy,
			},
			{
				name: "obtain a mainnet-valid signature from a testnet signer",
				intent: func() *Intent {
					i := validDepositIntent(time.Now().UTC())
					i.ID = "atk-4"
					i.NetworkPassphrase = otherNetwork
					return i
				}(),
				expect: RejectNetworkMismatch,
			},
			{
				name: "smuggle arguments into a permitted no-argument function",
				intent: &Intent{
					ID:                "atk-5",
					Operation:         OpPause,
					Shape:             ShapeVoid,
					ContractAddress:   testContract,
					NetworkPassphrase: testNetwork,
					Arg0:              500_000_000,
					IssuedAt:          time.Now().UTC(),
				},
				expect: RejectShapeMismatch,
			},
			{
				name: "replay a captured intent",
				intent: func() *Intent {
					i := validDepositIntent(time.Now().UTC())
					i.ID = "intent-1" // already signed during phase 0
					return i
				}(),
				expect: RejectIntentReplayed,
			},
		}

		for _, atk := range attacks {
			t.Run(atk.name, func(t *testing.T) {
				status, werr := rig.attemptSign(atk.intent)
				if status == http.StatusOK {
					t.Fatalf("ATTACK SUCCEEDED: %q was signed", atk.name)
				}
				if werr.Rejection != atk.expect {
					t.Fatalf("expected rejection %q, got %q", atk.expect, werr.Rejection)
				}
			})
		}

		// The decisive assertion: not one attack reached the signing key.
		if rig.backend.callCount() != signaturesBefore {
			t.Fatalf("an attack reached the signing key: %d signatures before, %d after",
				signaturesBefore, rig.backend.callCount())
		}
	})

	// ── Phase 2: detection ───────────────────────────────────────────────
	// Runbook §A claims rejections are the primary compromise signal. Verify
	// the data an on-call engineer would actually query is present.
	t.Run("phase2_detection_signals_are_present", func(t *testing.T) {
		snap := rig.service.Counters().Snapshot()

		var totalRejections int64
		for _, n := range snap.Rejected {
			totalRejections += n
		}
		if totalRejections < 6 {
			t.Fatalf("expected at least 6 recorded rejections, got %d", totalRejections)
		}

		// Every attack must be individually reconstructable from the audit
		// stream, since that is what §C blast-radius assessment depends on.
		events := rig.sink.all()
		seen := map[string]bool{}
		for _, ev := range events {
			if ev.Outcome == OutcomeRejected {
				seen[ev.IntentID] = true
			}
			if ev.IntentHash == "" {
				t.Fatalf("audit event %s carries no intent hash; the request could not be proven later", ev.IntentID)
			}
		}
		for _, id := range []string{"atk-1", "atk-2", "atk-3", "atk-4", "atk-5"} {
			if !seen[id] {
				t.Fatalf("attack %s produced no rejection audit event; it would be invisible during an incident", id)
			}
		}
	})

	// ── Phase 3: containment ─────────────────────────────────────────────
	// Runbook §B.1: the kill switch halts signing without a redeploy.
	t.Run("phase3_kill_switch_halts_signing", func(t *testing.T) {
		rig.engageKillSwitch("game day rehearsal")

		signaturesBefore := rig.backend.callCount()

		// Even a fully legitimate request must now be refused.
		legit := validDepositIntent(time.Now().UTC())
		legit.ID = "post-containment"
		status, werr := rig.attemptSign(legit)

		if status != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 while signing is halted, got %d", status)
		}
		if werr.Rejection != RejectKillSwitchActive {
			t.Fatalf("expected kill-switch rejection, got %q", werr.Rejection)
		}
		if rig.backend.callCount() != signaturesBefore {
			t.Fatal("a request reached the signing key while the kill switch was engaged")
		}

		// Runbook §B.1 requires the operator to be able to confirm the state.
		state, _, _ := rig.service.KillSwitchStatus()
		if state != StateDisabled {
			t.Fatalf("health status reports %q, an operator could not confirm containment", state)
		}
	})

	// ── Phase 4: containment is audited ──────────────────────────────────
	t.Run("phase4_containment_is_audited", func(t *testing.T) {
		ev, ok := rig.sink.last()
		if !ok {
			t.Fatal("no audit event for the kill-switch refusal")
		}
		if ev.Outcome != OutcomeDisabled || ev.Rejection != RejectKillSwitchActive {
			t.Fatalf("kill-switch refusal not audited correctly: outcome=%q rejection=%q",
				ev.Outcome, ev.Rejection)
		}
	})

	// ── Phase 5: the switch holds under failure ──────────────────────────
	// Runbook §B.1 claims the switch does not depend on the database or the
	// API. The rig has neither, so signing being halted here demonstrates it.
	t.Run("phase5_switch_holds_without_database_or_api", func(t *testing.T) {
		legit := validDepositIntent(time.Now().UTC())
		legit.ID = "no-deps"
		status, _ := rig.attemptSign(legit)
		if status != http.StatusServiceUnavailable {
			t.Fatalf("switch did not hold: got %d", status)
		}
	})

	// ── Phase 6: unauthorized callers ────────────────────────────────────
	// Simulates the attacker reaching the signer socket directly rather than
	// through the API.
	t.Run("phase6_unauthorized_caller_refused", func(t *testing.T) {
		rig.releaseKillSwitch()
		rig.callerErr = errors.New("no client certificate")
		defer func() { rig.callerErr = nil }()

		signaturesBefore := rig.backend.callCount()

		intent := validDepositIntent(time.Now().UTC())
		intent.ID = "unauthorized-caller"
		status, _ := rig.attemptSign(intent)

		if status != http.StatusUnauthorized {
			t.Fatalf("expected 401 for an unauthenticated caller, got %d", status)
		}
		if rig.backend.callCount() != signaturesBefore {
			t.Fatal("an unauthorized caller reached the signing key")
		}
		if rig.service.Counters().Snapshot().Unauthorized == 0 {
			t.Fatal("the authorization failure was not counted; it would be invisible")
		}
	})

	// ── Phase 7: recovery ────────────────────────────────────────────────
	// Runbook §E.3/§E.4: signing resumes, and recovery is verified by a real
	// operation rather than a health check alone.
	t.Run("phase7_recovery_restores_signing", func(t *testing.T) {
		rig.releaseKillSwitch()

		intent := validDepositIntent(time.Now().UTC())
		intent.ID = "post-recovery-verification"
		status, werr := rig.attemptSign(intent)

		if status != http.StatusOK {
			t.Fatalf("signing did not resume after recovery: status=%d err=%v", status, werr)
		}

		ev, _ := rig.sink.last()
		if ev.Outcome != OutcomeSigned {
			t.Fatalf("recovery verification was not audited as a signature: %q", ev.Outcome)
		}
		if ev.KeyID != "GOPERATORKEYFORREHEARSALONLY" {
			t.Fatalf("audit event does not identify the signing key: %q", ev.KeyID)
		}
	})

	// ── Phase 8: no secret leaked throughout ─────────────────────────────
	// Across every phase, nothing secret may have crossed the boundary or
	// entered the audit stream.
	t.Run("phase8_no_secrets_leaked_across_the_rehearsal", func(t *testing.T) {
		encoded, err := json.Marshal(rig.sink.all())
		if err != nil {
			t.Fatalf("marshal audit stream: %v", err)
		}
		stream := string(encoded)

		for _, forbidden := range []string{"SECRET", "Authorization", "Bearer ", "SIGNED_ENVELOPE_XDR"} {
			if strings.Contains(stream, forbidden) {
				t.Fatalf("the audit stream contains %q", forbidden)
			}
		}
	})
}

// TestGameDay_LatencyWithinBudget checks the boundary overhead claimed in
// docs/security/signing-isolation.md §7.
//
// It measures only what this change adds — validation, policy, replay, audit —
// with a stub backend, because build-and-simulate latency is dominated by the
// Soroban RPC round trip and is not part of the boundary's cost.
func TestGameDay_LatencyWithinBudget(t *testing.T) {
	backend := &stubBackend{}
	svc := newTestService(t, backend, &recordingSink{})

	const iterations = 200
	start := time.Now()
	for i := 0; i < iterations; i++ {
		intent := validDepositIntent(time.Now().UTC())
		intent.ID = "latency-" + itoa(int64(i))
		if _, err := svc.Sign(context.Background(), "api", intent); err != nil {
			t.Fatalf("sign %d: %v", i, err)
		}
	}
	elapsed := time.Since(start)
	perOp := elapsed / iterations

	// The documented budget for boundary overhead is under 5ms. The threshold
	// here is deliberately looser than the budget so the test reports a genuine
	// regression rather than flaking on a loaded CI runner.
	const threshold = 5 * time.Millisecond
	if perOp > threshold {
		t.Fatalf("signing boundary overhead is %v per operation, exceeding the %v budget",
			perOp, threshold)
	}
	t.Logf("signing boundary overhead: %v per operation over %d iterations", perOp, iterations)
}
