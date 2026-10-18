package stellar

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// AdminEventAlert represents an immediate high-priority page emitted when an
// admin-role or config-change event is observed on-chain (#1548).
type AdminEventAlert struct {
	EventID    string            `json:"event_id"`
	ContractID string            `json:"contract_id"`
	Topic      string            `json:"topic"`
	Category   string            `json:"category"`
	Severity   string            `json:"severity"`
	TxHash     string            `json:"tx_hash"`
	Signer     string            `json:"signer"`
	Diff       string            `json:"diff"`
	Ledger     uint64            `json:"ledger"`
	OccurredAt time.Time         `json:"occurred_at"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// AdminAlertSink delivers high-priority admin/config event alerts without
// waiting for a rate or count threshold (#1548).
type AdminAlertSink interface {
	SendAdminEventAlert(ctx context.Context, alert AdminEventAlert) error
}

type adminTopicSpec struct {
	Topic    string
	Category string
}

var adminConfigTopicRegistry = []adminTopicSpec{
	// Vault admin & config topics
	{Topic: "HARV_VLT", Category: "vault_admin"},
	{Topic: "CAP_CHG", Category: "vault_config"},
	{Topic: "PAUSE", Category: "vault_pause"},
	{Topic: "UNPAUSE", Category: "vault_pause"},
	// Registry admin topics
	{Topic: "SOURCE_ADDED", Category: "registry_config"},
	{Topic: "SOURCE_UPDATED", Category: "registry_config"},
	{Topic: "SOURCE_REMOVED", Category: "registry_config"},
	{Topic: "SRC_RECO", Category: "registry_recovery"},
	// Strategy config topics
	{Topic: "WEIGHTS_UPDATED", Category: "strategy_config"},
	// Access control & role topics
	{Topic: "ROLE_GRANTED", Category: "access_control"},
	{Topic: "ROLE_REVOKED", Category: "access_control"},
	{Topic: "ADMIN_TRANSFER", Category: "access_control"},
	{Topic: "RL_XFR_P", Category: "access_control"},
	{Topic: "RL_XFR_A", Category: "access_control"},
	{Topic: "RL_XFR_C", Category: "access_control"},
	{Topic: "RL_EXP", Category: "access_control"},
	// Circuit breaker manual/admin recovery
	{Topic: "BRK_RCVR", Category: "breaker_admin"},
	// Factory admin topics
	{Topic: "VLT_NEW", Category: "factory_admin"},
	{Topic: "VLT_DEP", Category: "factory_admin"},
	{Topic: "WASM_SET", Category: "factory_upgrade"},
	// Timelock governance topics
	{Topic: "PROPOSE", Category: "timelock_governance"},
	{Topic: "EXECUTE", Category: "timelock_governance"},
	{Topic: "CANCEL", Category: "timelock_governance"},
	// Contract WASM upgrade topics
	{Topic: "PROP_UPG", Category: "contract_upgrade"},
	{Topic: "CAN_UPG", Category: "contract_upgrade"},
	{Topic: "EXEC_UPG", Category: "contract_upgrade"},
}

var adminConfigTopicByNormalized = func() map[string]adminTopicSpec {
	m := make(map[string]adminTopicSpec, len(adminConfigTopicRegistry))
	for _, spec := range adminConfigTopicRegistry {
		m[strings.ToLower(spec.Topic)] = spec
	}
	return m
}()

// AdminConfigEventTopics returns the canonical list of all admin-role and
// config-change topics defined in packages/contracts/EVENTS.md (#1548).
func AdminConfigEventTopics() []string {
	out := make([]string, 0, len(adminConfigTopicRegistry))
	for _, spec := range adminConfigTopicRegistry {
		out = append(out, spec.Topic)
	}
	return out
}

// IsAdminConfigEventTopic reports whether eventType is a classified admin or
// config-change topic.
func IsAdminConfigEventTopic(eventType string) (string, string, bool) {
	spec, ok := adminConfigTopicByNormalized[normalizeEventTypeString(eventType)]
	if !ok {
		return "", "", false
	}
	return spec.Topic, spec.Category, true
}

// MaxSuppressionTTL bounds planned-change suppressions so a suppression entry
// can never permanently silence alerts.
const MaxSuppressionTTL = 2 * time.Hour

type plannedSuppression struct {
	ContractID string
	Topic      string
	Reason     string
	ExpiresAt  time.Time
}

// AdminEventClassifier inspects indexed events, checks short-lived planned
// change suppressions, and dispatches immediate high-priority alerts for every
// un-suppressed admin or config event (#1548).
type AdminEventClassifier struct {
	sink        AdminAlertSink
	now         func() time.Time
	mu          sync.Mutex
	suppressed  map[string]plannedSuppression
}

// NewAdminEventClassifier constructs an AdminEventClassifier wired to sink.
func NewAdminEventClassifier(sink AdminAlertSink) *AdminEventClassifier {
	return &AdminEventClassifier{
		sink:       sink,
		now:        func() time.Time { return time.Now().UTC() },
		suppressed: make(map[string]plannedSuppression),
	}
}

func suppressionKey(contractID, topic string) string {
	return strings.TrimSpace(contractID) + "::" + strings.ToUpper(strings.TrimSpace(topic))
}

// SuppressPlannedChange registers a short-lived suppression for a planned
// admin/config change on (contractID, topic). TTL is capped at MaxSuppressionTTL.
func (c *AdminEventClassifier) SuppressPlannedChange(contractID, topic, reason string, ttl time.Duration) error {
	canonicalTopic, _, ok := IsAdminConfigEventTopic(topic)
	if !ok {
		return fmt.Errorf("unknown admin/config topic %q", topic)
	}
	if strings.TrimSpace(contractID) == "" {
		return fmt.Errorf("contractID is required for suppression")
	}
	if ttl <= 0 {
		return fmt.Errorf("suppression ttl must be positive")
	}
	if ttl > MaxSuppressionTTL {
		ttl = MaxSuppressionTTL
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.suppressed[suppressionKey(contractID, canonicalTopic)] = plannedSuppression{
		ContractID: strings.TrimSpace(contractID),
		Topic:      canonicalTopic,
		Reason:     strings.TrimSpace(reason),
		ExpiresAt:  c.now().Add(ttl),
	}
	return nil
}

func (c *AdminEventClassifier) isSuppressed(contractID, canonicalTopic string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := suppressionKey(contractID, canonicalTopic)
	entry, ok := c.suppressed[key]
	if !ok {
		return false
	}
	if !c.now().Before(entry.ExpiresAt) {
		delete(c.suppressed, key)
		return false
	}
	// Consume one-shot planned change suppression once matched.
	delete(c.suppressed, key)
	return true
}

// InspectAndAlert evaluates an indexed event and fires a high-priority alert
// immediately if the event is an unsuppressed admin or config change.
func (c *AdminEventClassifier) InspectAndAlert(ctx context.Context, event indexedEvent) (bool, error) {
	if c == nil || c.sink == nil {
		return false, nil
	}
	canonicalTopic, category, ok := IsAdminConfigEventTopic(event.EventType)
	if !ok {
		return false, nil
	}

	if c.isSuppressed(event.ContractID, canonicalTopic) {
		return false, nil
	}

	alert := AdminEventAlert{
		EventID:    event.ID,
		ContractID: event.ContractID,
		Topic:      canonicalTopic,
		Category:   category,
		Severity:   "critical",
		TxHash:     event.TxHash,
		Signer:     extractAdminEventSigner(event),
		Diff:       formatAdminEventDiff(canonicalTopic, event.Data),
		Ledger:     event.Ledger,
		OccurredAt: eventOccurredAt(event),
	}

	if err := c.sink.SendAdminEventAlert(ctx, alert); err != nil {
		return false, err
	}
	return true, nil
}

func extractAdminEventSigner(event indexedEvent) string {
	signerKeys := []string{
		"admin",
		"grantor",
		"revoker",
		"proposer",
		"proposed_by",
		"executed_by",
		"cancelled_by",
		"authorised_by",
		"recovered_by",
		"caller",
		"actor",
		"from",
		"old_admin",
		"new_admin",
	}
	for _, k := range signerKeys {
		if val, ok := extractEventStringField(event, k); ok {
			return val
		}
	}
	return "unknown"
}

func formatAdminEventDiff(topic string, data map[string]any) string {
	if len(data) == 0 {
		return fmt.Sprintf("%s: {}", topic)
	}
	if oldVal, hasOld := data["old_value"]; hasOld {
		if newVal, hasNew := data["new_value"]; hasNew {
			field, _ := data["field"].(string)
			if field != "" {
				return fmt.Sprintf("%s(%s): %v -> %v", topic, field, oldVal, newVal)
			}
			return fmt.Sprintf("%s: %v -> %v", topic, oldVal, newVal)
		}
	}
	if oldSt, hasOld := data["old_status"]; hasOld {
		if newSt, hasNew := data["new_status"]; hasNew {
			return fmt.Sprintf("%s: %v -> %v", topic, oldSt, newSt)
		}
	}
	if fromVal, hasFrom := data["from"]; hasFrom {
		if toVal, hasTo := data["to"]; hasTo {
			return fmt.Sprintf("%s: %v -> %v", topic, fromVal, toVal)
		}
	}

	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, data[k]))
	}
	return fmt.Sprintf("%s: {%s}", topic, strings.Join(parts, ", "))
}

func notifyAdminEventClassifier(ctx context.Context, logger interface {
	Error(msg string, args ...any)
}, classifier *AdminEventClassifier, event indexedEvent) {
	if classifier == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil && logger != nil {
			logger.Error("admin event classifier: panic", "event_id", event.ID, "recovered", r)
		}
	}()
	if _, err := classifier.InspectAndAlert(ctx, event); err != nil && logger != nil {
		logger.Error("admin event classifier: failed to dispatch alert", "event_id", event.ID, "topic", event.EventType, "error", err)
	}
}
