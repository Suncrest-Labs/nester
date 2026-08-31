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

// MoneyPathSwitchRepository persists the pause switches.
type MoneyPathSwitchRepository interface {
	GetSwitch(ctx context.Context, op moneypath.Operation) (moneypath.Switch, error)
	ListSwitches(ctx context.Context) ([]moneypath.Switch, error)
	SetSwitch(ctx context.Context, op moneypath.Operation, paused bool, reason string, changedBy *uuid.UUID) (moneypath.Switch, error)
}

// switchCacheTTL bounds how long a released switch can still be enforced, and
// how long an engaged one can still let requests through.
//
// The issue asks for "effective within seconds". Reading the row on every
// deposit would be exact but puts a query on the hottest path in the system;
// two seconds is well inside the requirement and keeps the money path off the
// database for the common case where nothing is paused.
const switchCacheTTL = 2 * time.Second

// MoneyPathSwitchService reads and flips the global pause switches.
//
// Reads are cached briefly. Writes bypass and clear the cache, so an operator
// who engages a switch sees it enforced on the next request rather than up to
// a TTL later — the cache only ever delays the *discovery* of a change made
// elsewhere, such as by another instance.
type MoneyPathSwitchService struct {
	repository MoneyPathSwitchRepository
	audit      AuditLogger

	mu     sync.RWMutex
	cache  map[moneypath.Operation]cachedSwitch
	nowFn  func() time.Time
	logger func(context.Context, string, ...any)
}

type cachedSwitch struct {
	value     moneypath.Switch
	expiresAt time.Time
}

// NewMoneyPathSwitchService builds the service. audit may be nil, in which
// case changes are not recorded — acceptable only where no Postgres
// connection exists, matching NoopAuditLogger's role elsewhere.
func NewMoneyPathSwitchService(repository MoneyPathSwitchRepository, auditLogger AuditLogger) *MoneyPathSwitchService {
	if auditLogger == nil {
		auditLogger = NoopAuditLogger{}
	}
	return &MoneyPathSwitchService{
		repository: repository,
		audit:      auditLogger,
		cache:      make(map[moneypath.Operation]cachedSwitch),
		nowFn:      time.Now,
	}
}

// EnsureAllowed returns nil when op may proceed, and a *moneypath.PausedError
// carrying the operator's reason when it may not.
//
// It fails closed: if the switch cannot be read, the operation is refused.
// The switch exists to stop money moving during an incident, and a database
// the API cannot reach is itself an incident — defaulting to "allow" would
// make the control useless exactly when it is needed.
func (s *MoneyPathSwitchService) EnsureAllowed(ctx context.Context, op moneypath.Operation) error {
	if s == nil {
		return nil
	}
	if !op.Valid() {
		return moneypath.ErrUnknownOperation
	}

	state, err := s.get(ctx, op)
	if err != nil {
		// Fail closed, but do not lose why. The handler maps ErrPaused to a
		// 503 before it reaches any logging branch, so without this line an
		// outage looks exactly like a deliberate pause to whoever is on call.
		slog.Default().ErrorContext(ctx, "money path switch unreadable; refusing operation",
			"operation", string(op), "error", err.Error())
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

// Get reports the current state of one switch, bypassing the cache so an
// operator checking after a change sees the committed value.
func (s *MoneyPathSwitchService) Get(ctx context.Context, op moneypath.Operation) (moneypath.Switch, error) {
	if !op.Valid() {
		return moneypath.Switch{}, moneypath.ErrUnknownOperation
	}
	return s.repository.GetSwitch(ctx, op)
}

// List reports every switch.
func (s *MoneyPathSwitchService) List(ctx context.Context) ([]moneypath.Switch, error) {
	return s.repository.ListSwitches(ctx)
}

// SetPaused engages or releases a switch and records the change in the audit
// log. The audit write is best-effort: losing the log entry must not prevent
// an operator from stopping the money path during an incident.
func (s *MoneyPathSwitchService) SetPaused(
	ctx context.Context,
	op moneypath.Operation,
	paused bool,
	reason string,
	actor *uuid.UUID,
	ipAddress string,
) (moneypath.Switch, error) {
	if !op.Valid() {
		return moneypath.Switch{}, moneypath.ErrUnknownOperation
	}
	reason = strings.TrimSpace(reason)

	previous, err := s.repository.GetSwitch(ctx, op)
	if err != nil {
		return moneypath.Switch{}, err
	}

	updated, err := s.repository.SetSwitch(ctx, op, paused, reason, actor)
	if err != nil {
		return moneypath.Switch{}, err
	}

	// Drop the cached value so this instance enforces the new state
	// immediately rather than after the TTL.
	s.invalidate(op)

	action := "money_path.release"
	if paused {
		action = "money_path.pause"
	}
	_ = s.audit.Log(ctx, AuditEntry{
		UserID:     actor,
		Action:     action,
		EntityType: "money_path_switch",
		EntityID:   uuid.Nil,
		OldValue:   map[string]any{"operation": string(op), "paused": previous.Paused, "reason": previous.Reason},
		NewValue:   map[string]any{"operation": string(op), "paused": updated.Paused, "reason": updated.Reason},
		IPAddress:  ipAddress,
	})

	return updated, nil
}

func (s *MoneyPathSwitchService) get(ctx context.Context, op moneypath.Operation) (moneypath.Switch, error) {
	s.mu.RLock()
	entry, ok := s.cache[op]
	s.mu.RUnlock()
	if ok && s.nowFn().Before(entry.expiresAt) {
		return entry.value, nil
	}

	state, err := s.repository.GetSwitch(ctx, op)
	if err != nil {
		return moneypath.Switch{}, err
	}

	s.mu.Lock()
	s.cache[op] = cachedSwitch{value: state, expiresAt: s.nowFn().Add(switchCacheTTL)}
	s.mu.Unlock()

	return state, nil
}

func (s *MoneyPathSwitchService) invalidate(op moneypath.Operation) {
	s.mu.Lock()
	delete(s.cache, op)
	s.mu.Unlock()
}
