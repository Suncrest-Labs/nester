package handler

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/stellar"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// ProtocolUpgradeItem is the API representation of a pending or historical
// timelocked contract upgrade (#1549).
type ProtocolUpgradeItem struct {
	ContractAddress  string `json:"contract_address"`
	WasmHash         string `json:"wasm_hash"`
	ETA              int64  `json:"eta"`
	ETAISO           string `json:"eta_iso"`
	RemainingSeconds int64  `json:"remaining_seconds"`
	Proposer         string `json:"proposer"`
	Status           string `json:"status"`
	ProposedLedger   uint64 `json:"proposed_ledger"`
	TxHash           string `json:"tx_hash,omitempty"`
	ReleaseURL       string `json:"release_url"`
}

// ProtocolUpgradeStore abstracts persistence for timelocked contract upgrades.
type ProtocolUpgradeStore interface {
	ListUpgrades(ctx context.Context, status string) ([]stellar.PendingUpgrade, error)
}

// ProtocolUpgradeHandler serves GET /api/v1/protocol/upgrades (#1549).
type ProtocolUpgradeHandler struct {
	store ProtocolUpgradeStore
	now   func() time.Time
}

// NewProtocolUpgradeHandler constructs a ProtocolUpgradeHandler.
func NewProtocolUpgradeHandler(store ProtocolUpgradeStore) *ProtocolUpgradeHandler {
	return &ProtocolUpgradeHandler{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
	}
}

// Register mounts GET /api/v1/protocol/upgrades on mux.
func (h *ProtocolUpgradeHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/protocol/upgrades", h.listUpgrades)
}

func (h *ProtocolUpgradeHandler) listUpgrades(w http.ResponseWriter, r *http.Request) {
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status == "" {
		status = "pending"
	}
	switch status {
	case "pending", "cancelled", "executed", "all":
	default:
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("status must be one of: pending, cancelled, executed, all"))
		return
	}

	rows, err := h.store.ListUpgrades(r.Context(), status)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list protocol upgrades"))
		return
	}

	nowUnix := h.now().Unix()
	items := make([]ProtocolUpgradeItem, 0, len(rows))
	for _, row := range rows {
		rem := row.ETA - nowUnix
		if rem < 0 {
			rem = 0
		}
		etaISO := row.ETAISO
		if etaISO == "" && row.ETA > 0 {
			etaISO = time.Unix(row.ETA, 0).UTC().Format(time.RFC3339)
		}
		releaseURL := row.ReleaseURL
		if releaseURL == "" {
			releaseURL = stellar.DefaultReleaseURLForWasmHash(row.WasmHash)
		}
		items = append(items, ProtocolUpgradeItem{
			ContractAddress:  row.ContractAddress,
			WasmHash:         row.WasmHash,
			ETA:              row.ETA,
			ETAISO:           etaISO,
			RemainingSeconds: rem,
			Proposer:         row.Proposer,
			Status:           row.Status,
			ProposedLedger:   row.ProposedLedger,
			TxHash:           row.TxHash,
			ReleaseURL:       releaseURL,
		})
	}

	response.WriteJSON(w, http.StatusOK, response.OK(map[string]any{
		"upgrades": items,
	}))
}

// SQLProtocolUpgradeStore queries protocol_upgrades from PostgreSQL.
type SQLProtocolUpgradeStore struct {
	DB *sql.DB
}

// ListUpgrades returns upgrades matching status (or all when status == "all").
func (s *SQLProtocolUpgradeStore) ListUpgrades(ctx context.Context, status string) ([]stellar.PendingUpgrade, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	query := `
		SELECT contract_address, wasm_hash, eta, proposer, status, proposed_ledger, tx_hash, updated_at
		FROM protocol_upgrades
		WHERE ($1 = 'all' OR status = $1)
		ORDER BY eta ASC, updated_at DESC
	`
	rows, err := s.DB.QueryContext(ctx, query, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []stellar.PendingUpgrade
	for rows.Next() {
		var u stellar.PendingUpgrade
		if err := rows.Scan(
			&u.ContractAddress,
			&u.WasmHash,
			&u.ETA,
			&u.Proposer,
			&u.Status,
			&u.ProposedLedger,
			&u.TxHash,
			&u.OccurredAt,
		); err != nil {
			return nil, err
		}
		if u.ETA > 0 {
			u.ETAISO = time.Unix(u.ETA, 0).UTC().Format(time.RFC3339)
		}
		u.ReleaseURL = stellar.DefaultReleaseURLForWasmHash(u.WasmHash)
		out = append(out, u)
	}
	return out, rows.Err()
}
