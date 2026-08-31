package signing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// KillSwitch controls whether the signer will apply the key at all.
//
// Design constraints this satisfies, taken from the incident requirements:
//
//   - It must stop new signing immediately, without a redeploy. Editing code
//     and shipping a release is not an incident-response mechanism.
//   - It must not depend on the API process. If the API is the thing that has
//     been compromised, a switch reachable only through the API is worthless.
//   - It must fail closed. Any state in which the switch cannot be evaluated
//     is treated as "signing disabled", because the alternative — signing while
//     unable to confirm that signing is permitted — is precisely the failure
//     mode the switch exists to prevent.
//
// The mechanism is a sentinel file on a volume mounted into the signer
// container. An operator with shell access to the host or the container
// disables signing by creating the file. That deliberately requires host- or
// orchestrator-level authority rather than an application credential: the
// authority to halt signing should not be obtainable by compromising the
// application whose signing you are trying to halt.
type KillSwitch struct {
	// path is the sentinel file. Its presence means signing is disabled.
	path string

	mu sync.RWMutex
	// lastState caches the most recent evaluation so a transient stat error
	// does not have to re-derive everything, and so Status can report without
	// touching the filesystem.
	lastState  State
	lastReason string
	checkedAt  time.Time
}

// State is the kill switch position.
type State string

const (
	// StateEnabled means signing is permitted.
	StateEnabled State = "enabled"
	// StateDisabled means signing is halted by an explicit operator action.
	StateDisabled State = "disabled"
	// StateUnknown means the switch could not be evaluated. It is treated as
	// disabled at every decision point; it is a distinct value only so that
	// operators can tell "someone halted signing" from "the signer cannot tell
	// whether signing was halted", which call for different responses.
	StateUnknown State = "unknown"
)

// ErrSigningDisabled is returned when the kill switch blocks a signing request.
var ErrSigningDisabled = errors.New("signing is disabled by the kill switch")

// NewKillSwitch builds a switch backed by the sentinel file at path.
//
// An empty path is a configuration error rather than a reason to disable the
// feature: a signer that cannot be halted should not start. Callers should
// treat the returned error as fatal at startup.
func NewKillSwitch(path string) (*KillSwitch, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("kill switch sentinel path must be configured")
	}
	// The directory must exist and be reachable at startup. Discovering during
	// an incident that the switch was never wired up correctly is exactly the
	// failure this check prevents.
	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("kill switch directory %q is not reachable: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("kill switch path parent %q is not a directory", dir)
	}
	return &KillSwitch{path: path}, nil
}

// Check reports whether signing is currently permitted.
//
// It returns ErrSigningDisabled when the switch is engaged, and also when the
// switch state cannot be determined — the fail-closed rule. The returned State
// distinguishes the two for the caller audit record.
func (k *KillSwitch) Check() (State, error) {
	state, reason := k.evaluate()

	k.mu.Lock()
	k.lastState = state
	k.lastReason = reason
	k.checkedAt = time.Now().UTC()
	k.mu.Unlock()

	switch state {
	case StateEnabled:
		return state, nil
	case StateDisabled:
		return state, fmt.Errorf("%w: %s", ErrSigningDisabled, reason)
	default:
		// Fail closed. An unreadable switch is not permission to proceed.
		return StateUnknown, fmt.Errorf("%w: switch state undetermined (%s)", ErrSigningDisabled, reason)
	}
}

func (k *KillSwitch) evaluate() (State, string) {
	_, err := os.Stat(k.path)
	switch {
	case err == nil:
		return StateDisabled, "sentinel file present"
	case os.IsNotExist(err):
		// The expected steady state: no sentinel, signing permitted.
		return StateEnabled, "sentinel file absent"
	default:
		// Permission denied, an I/O error, a broken mount. We cannot establish
		// that signing is permitted, so we do not permit it.
		return StateUnknown, fmt.Sprintf("sentinel file could not be read: %v", err)
	}
}

// Status reports the last observed state without re-reading the filesystem,
// for health endpoints and operational display.
func (k *KillSwitch) Status() (State, string, time.Time) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.lastState, k.lastReason, k.checkedAt
}

// Path returns the configured sentinel path, for startup logging and for the
// runbook to reference a concrete location.
func (k *KillSwitch) Path() string {
	return k.path
}

// Engage disables signing by creating the sentinel file.
//
// This exists so the switch can be exercised by the game-day harness and by
// operational tooling running with signer-host authority. It is deliberately
// NOT reachable from the API process: nothing in the API imports a path that
// calls it, and the signer exposes no endpoint that invokes it. The supported
// production activation path remains creating the file directly, which the
// runbook documents.
func (k *KillSwitch) Engage(ctx context.Context, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// 0600: the sentinel records an operator decision and its contents are
	// read by humans during an incident. Nothing else needs access.
	f, err := os.OpenFile(k.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("engage kill switch: %w", err)
	}
	defer f.Close()

	// The sentinel body is operator-facing context, not a control input: the
	// switch is engaged by the file existing, never by what it contains. A
	// corrupt or empty body still disables signing.
	stamp := time.Now().UTC().Format(time.RFC3339)
	if _, err := fmt.Fprintf(f, "disabled_at=%s\nreason=%s\n", stamp, sanitizeReason(reason)); err != nil {
		return fmt.Errorf("write kill switch reason: %w", err)
	}
	return nil
}

// Release re-enables signing by removing the sentinel file.
//
// Recovery is deliberately a separate, explicit action. It carries the same
// authority requirement as engaging the switch.
func (k *KillSwitch) Release(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := os.Remove(k.path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("release kill switch: %w", err)
	}
	return nil
}

// sanitizeReason bounds and flattens operator-supplied text before it is
// written to the sentinel file, so a reason string cannot inject additional
// key=value lines that a careless reader might mistake for switch metadata.
func sanitizeReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "unspecified"
	}
	reason = strings.ReplaceAll(reason, "\n", " ")
	reason = strings.ReplaceAll(reason, "\r", " ")
	const maxReasonLen = 256
	if len(reason) > maxReasonLen {
		reason = reason[:maxReasonLen]
	}
	return reason
}
