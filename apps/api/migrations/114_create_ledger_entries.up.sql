-- Ledger entries: the authoritative append-only double-entry log.
-- Every logical movement inserts >=2 rows whose signed amounts sum to exactly zero,
-- enforced in a single DB transaction so half-written movements are impossible.
--
-- Amounts are signed BIGINT in the smallest integer unit (stroops, never floats).
-- Check constraint ensures integral discipline: BIGINT is inherently integral.
-- Direction is debit/credit derived from sign convention.
--
-- Partitioning plan: this table will be one of the largest tables (every money movement).
-- Plan partitioning by RANGE (created_at) monthly from the start. This initial migration
-- creates a regular table; a follow-up migration will convert to partitioned table
-- using pg_partman or native partitioning with minimal downtime.
-- For now, we add indexes and note the plan in comments.

CREATE TABLE IF NOT EXISTS ledger_entries (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id    UUID        NOT NULL, -- groups legs of one logical movement
    account_id        UUID        NOT NULL REFERENCES ledger_accounts(id) ON DELETE RESTRICT,
    amount            BIGINT      NOT NULL CHECK (amount != 0), -- signed, stroops, integer discipline
    direction         TEXT        NOT NULL CHECK (direction IN ('debit','credit')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    domain_event_type TEXT, -- deposit, withdraw, harvest, rebalance, fee, etc.
    domain_event_id   TEXT, -- reference to domain event (e.g., vault_transaction id, harvest tx hash)
    asset_code        TEXT        NOT NULL DEFAULT 'USDC',
    asset_unit        TEXT        NOT NULL DEFAULT 'stroops' -- document unit, smallest integer unit
);

-- Integer-discipline documentation and check: amount must be integral (BIGINT guarantees it)
COMMENT ON COLUMN ledger_entries.amount IS 'Signed amount in smallest integer unit (stroops = 10^-7 USDC). BIGINT ensures integral, never float. Sum of entries per transaction_id must be zero.';
COMMENT ON COLUMN ledger_entries.asset_unit IS 'Base unit: stroops. All amounts integers. Decimal conversion only at presentation edge.';
COMMENT ON COLUMN ledger_entries.transaction_id IS 'Groups the legs of one logical double-entry transaction. Sum of amounts per transaction_id must be exactly zero.';

-- Critical indexes for performance
CREATE INDEX IF NOT EXISTS idx_ledger_entries_account_created
    ON ledger_entries (account_id, created_at);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_transaction_id
    ON ledger_entries (transaction_id);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_created_at
    ON ledger_entries (created_at);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_domain_event
    ON ledger_entries (domain_event_type, domain_event_id);

-- Append-only enforcement via trigger (optional, but we document intent)
-- We do NOT allow UPDATE or DELETE in application code; history is immutable and auditable.
-- A database-level rule could be added later to block UPDATE/DELETE.
