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
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubBackend stands in for the real signing key.
//
// It records what it was asked to sign so tests can prove that a rejected
// intent never reached the key at all — the distinction between "refused" and
// "signed but then errored" is the whole point of the boundary.
type stubBackend struct {
	mu     sync.Mutex
	calls  []Intent
	keyID  string
	result string
	err    error
}

func (b *stubBackend) KeyID() string {
	if b.keyID == "" {
		return "GTESTKEYIDENTIFIER"
	}
	return b.keyID
}

func (b *stubBackend) BuildAndSign(_ context.Context, i *Intent) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, *i)
	if b.err != nil {
		return "", b.err
	}
	if b.result == "" {
		return "SIGNED_ENVELOPE_XDR", nil
	}
	return b.result, nil
}

func (b *stubBackend) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.calls)
}

// recordingSink captures audit events for assertion.
type recordingSink struct {
	mu     sync.Mutex
	events []Event
	err    error
}

func (s *recordingSink) Record(_ context.Context, ev Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
	return s.err
}

func (s *recordingSink) all() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}

func (s *recordingSink) last() (Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) == 0 {
		return Event{}, false
	}
	return s.events[len(s.events)-1], true
}

// newTestKillSwitch builds a switch over a temp directory, returning the
// sentinel path so tests can engage it by creating the file.
func newTestKillSwitch(t *testing.T) (*KillSwitch, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "signing.disabled")
	ks, err := NewKillSwitch(path)
	if err != nil {
		t.Fatalf("build kill switch: %v", err)
	}
	return ks, path
}

func newTestService(t *testing.T, backend Backend, sink Sink) *Service {
	t.Helper()
	ks, _ := newTestKillSwitch(t)
	return newTestServiceWithSwitch(t, backend, sink, ks)
}

func newTestServiceWithSwitch(t *testing.T, backend Backend, sink Sink, ks *KillSwitch) *Service {
	t.Helper()
	svc, err := NewService(ServiceOptions{
		Backend:    backend,
		Policy:     testPolicy(),
		KillSwitch: ks,
		Sink:       sink,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("build service: %v", err)
	}
	return svc
}

func TestServiceSignsValidIntent(t *testing.T) {
	backend := &stubBackend{}
	sink := &recordingSink{}
	svc := newTestService(t, backend, sink)

	result, err := svc.Sign(context.Background(), "api", validDepositIntent(time.Now().UTC()))
	if err != nil {
		t.Fatalf("valid intent was rejected: %v", err)
	}
	if result.SignedXDR != "SIGNED_ENVELOPE_XDR" {
		t.Fatalf("unexpected signed envelope: %q", result.SignedXDR)
	}
	if result.IntentHash == "" {
		t.Fatal("result carries no intent hash, so the audit record could not be correlated")
	}

	ev, ok := sink.last()
	if !ok {
		t.Fatal("signing produced no audit event")
	}
	if ev.Outcome != OutcomeSigned {
		t.Fatalf("expected outcome %q, got %q", OutcomeSigned, ev.Outcome)
	}
	if ev.KeyID == "" {
		t.Fatal("audit event does not record which key signed")
	}
}

func TestServiceRejectionNeverReachesTheKey(t *testing.T) {
	// The core isolation property: a refused intent must not reach the backend
	// that holds the key. A signer that validated *after* signing would leak
	// signatures for rejected requests.
	cases := map[string]func(*Intent){
		"unknown operation": func(i *Intent) { i.Operation = "drain" },
		"wrong contract":    func(i *Intent) { i.ContractAddress = otherContract },
		"wrong network":     func(i *Intent) { i.NetworkPassphrase = otherNetwork },
		"excessive amount":  func(i *Intent) { i.Arg0 = 999_999_999_999 },
		"expired":           func(i *Intent) { i.IssuedAt = time.Now().UTC().Add(-time.Hour) },
		"shape mismatch":    func(i *Intent) { i.Shape = ShapeVoid },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			backend := &stubBackend{}
			sink := &recordingSink{}
			svc := newTestService(t, backend, sink)

			intent := validDepositIntent(time.Now().UTC())
			mutate(intent)

			if _, err := svc.Sign(context.Background(), "api", intent); err == nil {
				t.Fatal("intent was SIGNED when it should have been rejected")
			}
			if backend.callCount() != 0 {
				t.Fatalf("rejected intent reached the signing key (%d backend calls)", backend.callCount())
			}

			ev, ok := sink.last()
			if !ok {
				t.Fatal("rejection produced no audit event; a compromise would be invisible")
			}
			if ev.Outcome != OutcomeRejected {
				t.Fatalf("expected outcome %q, got %q", OutcomeRejected, ev.Outcome)
			}
			if ev.Rejection == "" {
				t.Fatal("rejection audit event carries no category")
			}
		})
	}
}

func TestServiceRejectsReplayedIntent(t *testing.T) {
	backend := &stubBackend{}
	sink := &recordingSink{}
	svc := newTestService(t, backend, sink)

	intent := validDepositIntent(time.Now().UTC())
	if _, err := svc.Sign(context.Background(), "api", intent); err != nil {
		t.Fatalf("first signature failed: %v", err)
	}

	// The same intent, replayed. It must not produce a second signature.
	_, err := svc.Sign(context.Background(), "api", intent)
	assertRejected(t, err, RejectIntentReplayed)

	if backend.callCount() != 1 {
		t.Fatalf("replayed intent produced %d signatures, expected 1", backend.callCount())
	}
}

func TestKillSwitchBlocksSigning(t *testing.T) {
	backend := &stubBackend{}
	sink := &recordingSink{}
	ks, sentinel := newTestKillSwitch(t)
	svc := newTestServiceWithSwitch(t, backend, sink, ks)

	// Signing works before the switch is engaged.
	if _, err := svc.Sign(context.Background(), "api", validDepositIntent(time.Now().UTC())); err != nil {
		t.Fatalf("signing failed before the kill switch was engaged: %v", err)
	}
	signedBefore := backend.callCount()

	// Engage: create the sentinel, exactly as an operator would during an
	// incident. No redeploy, no restart.
	if err := os.WriteFile(sentinel, []byte("incident-test"), 0o600); err != nil {
		t.Fatalf("engage kill switch: %v", err)
	}

	intent := validDepositIntent(time.Now().UTC())
	intent.ID = "post-killswitch"
	_, err := svc.Sign(context.Background(), "api", intent)
	if err == nil {
		t.Fatal("signing succeeded while the kill switch was engaged")
	}
	if !errors.Is(err, ErrSigningDisabled) {
		t.Fatalf("expected ErrSigningDisabled, got %v", err)
	}
	if backend.callCount() != signedBefore {
		t.Fatal("a request reached the signing key while the kill switch was engaged")
	}

	ev, ok := sink.last()
	if !ok {
		t.Fatal("kill-switch refusal produced no audit event")
	}
	if ev.Outcome != OutcomeDisabled || ev.Rejection != RejectKillSwitchActive {
		t.Fatalf("expected a kill-switch audit event, got outcome=%q rejection=%q", ev.Outcome, ev.Rejection)
	}
}

func TestKillSwitchRecoveryRestoresSigning(t *testing.T) {
	backend := &stubBackend{}
	ks, sentinel := newTestKillSwitch(t)
	svc := newTestServiceWithSwitch(t, backend, &recordingSink{}, ks)

	if err := os.WriteFile(sentinel, []byte("halt"), 0o600); err != nil {
		t.Fatalf("engage: %v", err)
	}
	if _, err := svc.Sign(context.Background(), "api", validDepositIntent(time.Now().UTC())); err == nil {
		t.Fatal("signing worked while disabled")
	}

	// Recovery is the deliberate removal of the sentinel.
	if err := ks.Release(context.Background()); err != nil {
		t.Fatalf("release: %v", err)
	}

	intent := validDepositIntent(time.Now().UTC())
	intent.ID = "post-recovery"
	if _, err := svc.Sign(context.Background(), "api", intent); err != nil {
		t.Fatalf("signing did not resume after recovery: %v", err)
	}
}

func TestKillSwitchEngageAndReleaseRoundTrip(t *testing.T) {
	ks, sentinel := newTestKillSwitch(t)
	ctx := context.Background()

	if state, err := ks.Check(); err != nil || state != StateEnabled {
		t.Fatalf("expected enabled with no sentinel, got state=%q err=%v", state, err)
	}

	if err := ks.Engage(ctx, "game day rehearsal"); err != nil {
		t.Fatalf("engage: %v", err)
	}
	if state, err := ks.Check(); err == nil || state != StateDisabled {
		t.Fatalf("expected disabled after engage, got state=%q err=%v", state, err)
	}

	// The sentinel records why, for the operator reading it at 3am.
	body, err := os.ReadFile(sentinel) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	if !strings.Contains(string(body), "game day rehearsal") {
		t.Fatalf("sentinel does not record the reason: %q", body)
	}

	if err := ks.Release(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}
	if state, err := ks.Check(); err != nil || state != StateEnabled {
		t.Fatalf("expected enabled after release, got state=%q err=%v", state, err)
	}
}

func TestKillSwitchReleaseIsIdempotent(t *testing.T) {
	ks, _ := newTestKillSwitch(t)
	// Releasing a switch that was never engaged must not error: during
	// recovery an operator may not know the current state.
	if err := ks.Release(context.Background()); err != nil {
		t.Fatalf("releasing an unengaged switch errored: %v", err)
	}
}

func TestKillSwitchSanitisesReason(t *testing.T) {
	ks, sentinel := newTestKillSwitch(t)
	// A reason containing newlines could otherwise inject additional
	// key=value lines that a reader might mistake for switch metadata.
	if err := ks.Engage(context.Background(), "line one\nreason=fake\nmore"); err != nil {
		t.Fatalf("engage: %v", err)
	}
	body, err := os.ReadFile(sentinel) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	// Exactly two lines: disabled_at and reason.
	lines := strings.Count(strings.TrimSpace(string(body)), "\n") + 1
	if lines != 2 {
		t.Fatalf("expected 2 metadata lines, got %d: %q", lines, body)
	}
}

func TestKillSwitchRequiresConfiguredPath(t *testing.T) {
	// A signer that cannot be halted must not start.
	if _, err := NewKillSwitch(""); err == nil {
		t.Fatal("an empty kill switch path was accepted")
	}
	if _, err := NewKillSwitch(filepath.Join("definitely", "missing", "dir", "sentinel")); err == nil {
		t.Fatal("an unreachable kill switch directory was accepted")
	}
}

func TestServiceRequiresPolicyAndKillSwitch(t *testing.T) {
	ks, _ := newTestKillSwitch(t)
	backend := &stubBackend{}

	if _, err := NewService(ServiceOptions{Policy: testPolicy(), KillSwitch: ks}); err == nil {
		t.Fatal("a service with no backend was accepted")
	}
	if _, err := NewService(ServiceOptions{Backend: backend, KillSwitch: ks}); err == nil {
		t.Fatal("a service with no policy was accepted; it would sign anything")
	}
	if _, err := NewService(ServiceOptions{Backend: backend, Policy: testPolicy()}); err == nil {
		t.Fatal("a service with no kill switch was accepted; it could not be halted")
	}
}

func TestAuditSinkFailureDoesNotBlockSigning(t *testing.T) {
	// An audit database outage must not become an availability outage. The
	// event still reaches the structured log through the fallback sink.
	backend := &stubBackend{}
	sink := &recordingSink{err: errors.New("audit store unavailable")}
	svc := newTestService(t, backend, sink)

	if _, err := svc.Sign(context.Background(), "api", validDepositIntent(time.Now().UTC())); err != nil {
		t.Fatalf("signing failed because the audit sink errored: %v", err)
	}
	if len(sink.all()) != 1 {
		t.Fatal("the event was not offered to the sink")
	}
}

func TestAuditEventsCarryNoSecrets(t *testing.T) {
	// Explicit check against the prohibition on secrets in audit records.
	backend := &stubBackend{keyID: "GOPERATORPUBLICADDRESS"}
	sink := &recordingSink{}
	svc := newTestService(t, backend, sink)

	if _, err := svc.Sign(context.Background(), "api", validDepositIntent(time.Now().UTC())); err != nil {
		t.Fatalf("sign: %v", err)
	}
	ev, _ := sink.last()

	encoded, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	serialised := string(encoded)

	// A Stellar secret seed starts with S and is 56 characters. The signed
	// envelope must not be present either.
	for _, forbidden := range []string{"SECRET", "SIGNED_ENVELOPE_XDR", "Authorization", "Bearer "} {
		if strings.Contains(serialised, forbidden) {
			t.Fatalf("audit event contains %q: %s", forbidden, serialised)
		}
	}
	// The key IDENTIFIER is expected and is public data.
	if !strings.Contains(serialised, "GOPERATORPUBLICADDRESS") {
		t.Fatal("audit event does not identify which key signed")
	}
}

func TestCountersTrackOutcomes(t *testing.T) {
	backend := &stubBackend{}
	svc := newTestService(t, backend, &recordingSink{})

	if _, err := svc.Sign(context.Background(), "api", validDepositIntent(time.Now().UTC())); err != nil {
		t.Fatalf("sign: %v", err)
	}
	bad := validDepositIntent(time.Now().UTC())
	bad.ID = "bad-1"
	bad.ContractAddress = otherContract
	_, _ = svc.Sign(context.Background(), "api", bad)

	snap := svc.Counters().Snapshot()
	if snap.Signed[OpDeposit] != 1 {
		t.Fatalf("expected 1 signed deposit, got %d", snap.Signed[OpDeposit])
	}
	if snap.Rejected[RejectContractNotAllowed] != 1 {
		t.Fatalf("expected 1 contract rejection, got %d", snap.Rejected[RejectContractNotAllowed])
	}
	if snap.Requests[OpDeposit] != 2 {
		t.Fatalf("expected 2 deposit requests, got %d", snap.Requests[OpDeposit])
	}
}

// ── transport tests ──────────────────────────────────────────────────────────

func newTestServer(t *testing.T, backend Backend, identity func(*http.Request) (string, error)) *httptest.Server {
	t.Helper()
	svc := newTestService(t, backend, &recordingSink{})
	srv := NewServer(svc, identity, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestUnauthorizedCallerCannotInvokeSigning(t *testing.T) {
	backend := &stubBackend{}
	// An identity resolver that refuses everything, standing in for a caller
	// with no valid client certificate or no socket access.
	ts := newTestServer(t, backend, func(*http.Request) (string, error) {
		return "", errors.New("no client certificate")
	})

	body, err := json.Marshal(wireRequest{Intent: validDepositIntent(time.Now().UTC())})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(ts.URL+SignPath, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an unauthenticated caller, got %d", resp.StatusCode)
	}
	if backend.callCount() != 0 {
		t.Fatal("an unauthorized request reached the signing key")
	}
}

func TestAuthorizedCallerCanInvokeSigning(t *testing.T) {
	backend := &stubBackend{}
	ts := newTestServer(t, backend, func(*http.Request) (string, error) {
		return "nester-api", nil
	})

	body, err := json.Marshal(wireRequest{Intent: validDepositIntent(time.Now().UTC())})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(ts.URL+SignPath, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 for an authorized caller, got %d: %s", resp.StatusCode, payload)
	}
	var decoded wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.SignedXDR == "" {
		t.Fatal("authorized caller received no signed transaction")
	}
}

func TestServerRefusesWhenNoIdentityResolverConfigured(t *testing.T) {
	// The default must be to refuse: a server that cannot identify its callers
	// must not sign for them.
	backend := &stubBackend{}
	svc := newTestService(t, backend, &recordingSink{})
	srv := NewServer(svc, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body, _ := json.Marshal(wireRequest{Intent: validDepositIntent(time.Now().UTC())})
	resp, err := http.Post(ts.URL+SignPath, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no identity resolver, got %d", resp.StatusCode)
	}
	if backend.callCount() != 0 {
		t.Fatal("a request reached the key with no identity resolver configured")
	}
}

func TestSignerExposesNoKeyExportRoute(t *testing.T) {
	// There must be no endpoint that returns key material. This asserts the
	// absence directly rather than trusting that none was added.
	backend := &stubBackend{}
	ts := newTestServer(t, backend, func(*http.Request) (string, error) { return "api", nil })

	for _, path := range []string{"/v1/key", "/v1/export", "/v1/secret", "/key", "/export"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("route %s exists and returned 200: %s", path, body)
		}
	}
}

func TestHealthEndpointReportsKillSwitchWithoutSecrets(t *testing.T) {
	backend := &stubBackend{keyID: "GOPERATORPUBLIC"}
	ts := newTestServer(t, backend, func(*http.Request) (string, error) { return "api", nil })

	resp, err := http.Get(ts.URL + HealthPath)
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from health, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "kill_switch") {
		t.Fatalf("health does not report the kill switch: %s", body)
	}
	if strings.Contains(string(body), "SECRET") {
		t.Fatalf("health response leaked something secret-shaped: %s", body)
	}
}

func TestPolicyRejectionMapsToUnprocessable(t *testing.T) {
	backend := &stubBackend{}
	ts := newTestServer(t, backend, func(*http.Request) (string, error) { return "api", nil })

	intent := validDepositIntent(time.Now().UTC())
	intent.ContractAddress = otherContract
	body, _ := json.Marshal(wireRequest{Intent: intent})

	resp, err := http.Post(ts.URL+SignPath, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for a policy rejection, got %d", resp.StatusCode)
	}
	var werr wireError
	if err := json.NewDecoder(resp.Body).Decode(&werr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if werr.Rejection != RejectContractNotAllowed {
		t.Fatalf("expected category %q, got %q", RejectContractNotAllowed, werr.Rejection)
	}
}

func TestOversizedRequestBodyRejected(t *testing.T) {
	backend := &stubBackend{}
	ts := newTestServer(t, backend, func(*http.Request) (string, error) { return "api", nil })

	huge := strings.Repeat("A", maxRequestBytes+1024)
	resp, err := http.Post(ts.URL+SignPath, "application/json", strings.NewReader(huge))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("an oversized body was accepted")
	}
	if backend.callCount() != 0 {
		t.Fatal("an oversized request reached the signing key")
	}
}

func TestSigningLatencyIsRecorded(t *testing.T) {
	// The signing path is on the operational hot path, so the boundary must
	// report how long it took. This asserts the measurement exists; the budget
	// itself is documented in docs/security/signing-isolation.md.
	backend := &stubBackend{}
	sink := &recordingSink{}
	svc := newTestService(t, backend, sink)

	if _, err := svc.Sign(context.Background(), "api", validDepositIntent(time.Now().UTC())); err != nil {
		t.Fatalf("sign: %v", err)
	}
	ev, _ := sink.last()
	if ev.LatencyMS < 0 {
		t.Fatalf("negative latency recorded: %d", ev.LatencyMS)
	}
	snap := svc.Counters().Snapshot()
	if snap.LatencySamples != 1 {
		t.Fatalf("expected 1 latency sample, got %d", snap.LatencySamples)
	}
}
