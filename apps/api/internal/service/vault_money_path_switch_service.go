package service

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/moneypath"
)

// VaultMoneyPathSwitchRepository persists the per-vault pause switches
// (nester#1322).
type VaultMoneyPathSwitchRepository interface {
	GetVaultSwitch(ctx context.Context, vaultID uuid.UUID, op moneypath.Operation) (moneypath.VaultSwitch, error)
	ListVaultSwitches(ctx context.Context, vaultID uuid.UUID) ([]moneypath.VaultSwitch, error)
	SetVaultSwitch(ctx context.Context, vaultID uuid.UUID, op moneypath.Operation, paused bool, reason string, changedBy *uuid.UUID) (moneypath.VaultSwitch, error)
}

// vaultSwitchCacheTTL bounds how long a per-vault switch change made on
// another instance can go unseen here, and how long a release can still let
// requests through.
//
// The global switches use the same TTL for the same reason: reading the row
// on every deposit is exact but puts a query on the hottest path in the
// system, and two seconds is well inside the "effective within seconds"
// requirement this control exists to meet.
const vaultSwitchCacheTTL = 2 * time.Second

// VaultMoneyPathSwitchService reads and flips the per-vault pause switches.
//
// It is the per-vault counterpart of MoneyPathSwitchService. The global
// service stops an operation across the whole protocol; this one stops it on
// exactly one vault, so containing a single misbehaving vault no longer
// means halting every other vault with it.
//
// Reads are cached briefly. Writes bypass and clear the cache, so an operator
// who engages a switch sees it enforced on the next request rather than up to
// a TTL later.
type VaultMoneyPathSwitchService struct {
	repository VaultMoneyPathSwitchRepository
	audit      AuditLogger

	mu    sync.RWMutex
	cache map[moneypath.VaultSwitchKey]cachedVaultSwitch
	nowFn func() time.Time
}

type cachedVaultSwitch struct {
	value     moneypath.VaultSwitch
	expiresAt time.Time
}

// NewVaultMoneyPathSwitchService builds the service. audit may be nil, in
// which case changes are not recorded — acceptable only where no Postgres
// connection exists, matching NoopAuditLogger's role elsewhere.
func NewVaultMoneyPathSwitchService(repository VaultMoneyPathSwitchRepository, auditLogger AuditLogger) *VaultMoneyPathSwitchService {
	if auditLogger == nil {
		auditLogger = NoopAuditLogger{}
	}
	return &VaultMoneyPathSwitchService{
		repository: repository,
		audit:      auditLogger,
		cache:      make(map[moneypath.VaultSwitchKey]cachedVaultSwitch),
		nowFn:      time.Now,
	}
}

// EnsureVaultAllowed returns nil when vaultID may perform op, and a
// *moneypath.PausedError carrying the operator's reason when it may not.
//
// A nil service, a nil vault id, or a vault with no stored row all allow the
// operation: absence of a switch is not a pause, and validation of a missing
// vault id belongs to the caller. It fails closed only when the switch
// cannot be read — matching the global gate, because a database the API
// cannot reach is itself an incident and defaulting to "allow" would make
// the control useless exactly when it is needed.
func (s *VaultMoneyPathSwitchService) EnsureVaultAllowed(ctx context.Context, vaultID uuid.UUID, op moneypath.Operation) error {
	if s == nil {
		return nil
	}
	if !op.Valid() {
		return moneypath.ErrUnknownOperation
	}
	if vaultID == uuid.Nil {
		return nil
	}

	state, err := s.get(ctx, vaultID, op)
	if err != nil {
		// Fail closed, but do not lose why. The handler maps ErrPaused to a
		// 503 before it reaches any logging branch, so without this line an
		// outage looks exactly like a deliberate pause to whoever is on call.
		slog.Default().ErrorContext(ctx, "vault money path switch unreadable; refusing operation",
			"vault_id", vaultID.String(), "operation", string(op), "error", err.Error())
		return &moneypath.PausedError{
			Operation: op,
			Reason:    "pause state is currently unreadable; refusing the operation until it can be confirmed",
			Cause:     err,
		}
	}
	if state.Paused {
		return &moneypath.PausedError{Operation: op, Reason: state.Reason}
	}
	return nil
}

// List reports the state of every operation on one vault, including
// operations with no stored row (reported as released), so an admin view
// always shows both switches rather than only the ones someone has touched.
func (s *VaultMoneyPathSwitchService) List(ctx context.Context, vaultID uuid.UUID) ([]moneypath.VaultSwitch, error) {
	if vaultID == uuid.Nil {
		return nil, ErrInvalidAdminInput
	}

	rows, err := s.repository.ListVaultSwitches(ctx, vaultID)
	if err != nil {
		return nil, err
	}

	stored := make(map[moneypath.Operation]moneypath.VaultSwitch, len(rows))
	for _, row := range rows {
		stored[row.Operation] = row
	}

	out := make([]moneypath.VaultSwitch, 0, len(moneypath.Operations()))
	for _, op := range moneypath.Operations() {
		if row, ok := stored[op]; ok {
			out = append(out, row)
			continue
		}
		out = append(out, moneypath.VaultSwitch{VaultID: vaultID, Operation: op})
	}
	return out, nil
}

// SetPaused engages or releases one vault's switch and records the change in
// the audit log. The audit write is best-effort: losing the log entry must
// not prevent an operator from stopping the money path during an incident.
func (s *VaultMoneyPathSwitchService) SetPaused(
	ctx context.Context,
	vaultID uuid.UUID,
	op moneypath.Operation,
	paused bool,
	reason string,
	actor *uuid.UUID,
	ipAddress string,
) (moneypath.VaultSwitch, error) {
	if vaultID == uuid.Nil {
		return moneypath.VaultSwitch{}, ErrInvalidAdminInput
	}
	if !op.Valid() {
		return moneypath.VaultSwitch{}, moneypath.ErrUnknownOperation
	}
	reason = strings.TrimSpace(reason)

	// The old state is read only to annotate the audit log. A transient
	// failure here must not block the write below: during an incident, an
	// operator trying to pause a vault must not be refused because of an
	// unrelated read hiccup. If it fails, the audit entry records "unknown"
	// for the old value instead of returning early.
	previous, prevErr := s.repository.GetVaultSwitch(ctx, vaultID, op)
	if prevErr != nil {
		slog.Default().WarnContext(ctx, "vault money path switch: could not read previous state for audit log; proceeding with write",
			"vault_id", vaultID.String(), "operation", string(op), "error", prevErr.Error())
	}

	updated, err := s.repository.SetVaultSwitch(ctx, vaultID, op, paused, reason, actor)
	if err != nil {
		return moneypath.VaultSwitch{}, err
	}

	// Overwrite the cached value with the new state so a concurrent get()
	// can never repopulate the cache with the stale pre-write value: there
	// is no gap between "invalidated" and "holds the new value" for a racing
	// read to land in.
	s.set(vaultID, op, updated)

	action := "vault_money_path.release"
	if paused {
		action = "vault_money_path.pause"
	}
	oldValue := map[string]any{"operation": string(op), "paused": "unknown", "reason": "unknown"}
	if prevErr == nil {
		oldValue = map[string]any{"operation": string(op), "paused": previous.Paused, "reason": previous.Reason}
	}
	_ = s.audit.Log(ctx, AuditEntry{
		UserID:     actor,
		Action:     action,
		EntityType: "vault_money_path_switch",
		EntityID:   vaultID,
		OldValue:   oldValue,
		NewValue:   map[string]any{"operation": string(op), "paused": updated.Paused, "reason": updated.Reason},
		IPAddress:  ipAddress,
	})

	return updated, nil
}

func (s *VaultMoneyPathSwitchService) get(ctx context.Context, vaultID uuid.UUID, op moneypath.Operation) (moneypath.VaultSwitch, error) {
	key := moneypath.VaultSwitchKey{VaultID: vaultID, Operation: op}

	s.mu.RLock()
	entry, ok := s.cache[key]
	s.mu.RUnlock()
	if ok && s.nowFn().Before(entry.expiresAt) {
		return entry.value, nil
	}

	state, err := s.repository.GetVaultSwitch(ctx, vaultID, op)
	if err != nil {
		return moneypath.VaultSwitch{}, err
	}

	s.mu.Lock()
	s.cache[key] = cachedVaultSwitch{value: state, expiresAt: s.nowFn().Add(vaultSwitchCacheTTL)}
	s.mu.Unlock()

	return state, nil
}

// set writes value directly into the cache under the full TTL, used after a
// write so the new state is what any concurrent or subsequent get() sees.
// Writing the new value rather than deleting the entry closes the race where
// a concurrent get() that already read the stale pre-write row from the
// repository would otherwise repopulate the cache with that stale value
// after a plain invalidation.
func (s *VaultMoneyPathSwitchService) set(vaultID uuid.UUID, op moneypath.Operation, value moneypath.VaultSwitch) {
	key := moneypath.VaultSwitchKey{VaultID: vaultID, Operation: op}
	s.mu.Lock()
	s.cache[key] = cachedVaultSwitch{value: value, expiresAt: s.nowFn().Add(vaultSwitchCacheTTL)}
	s.mu.Unlock()
}
