-- Per-vault pause switch for the money path (nester#1322).
--
-- The global switches in migration 106 stop deposits or withdrawals across
-- the whole protocol. That is the correct lever for a protocol-wide
-- incident, but too blunt when a single vault is misbehaving: halting every
-- vault to contain one is a self-inflicted outage.
--
-- This table scopes the same control to one vault. A row exists only for a
-- vault whose switch has been touched at least once; the absence of a row
-- means "not paused" (unlike the global switches, which are seeded). The
-- service reads per (vault_id, operation) and fails closed only on a read
-- error, never on an absent row.
--
-- Deposits and withdrawals are separate rows so they can be halted
-- independently, matching the global switches: the common case is stopping
-- new money entering one vault while still letting its users take theirs
-- out.
CREATE TABLE IF NOT EXISTS vault_money_path_switches (
    -- The vault this switch governs. Cascades so closing a vault does not
    -- leave pause rows behind that a future vault id could never reuse, but
    -- that would otherwise accumulate.
    vault_id    UUID        NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,

    -- The operation this switch governs. The CHECK keeps a typo from
    -- creating a switch nothing enforces.
    operation   TEXT        NOT NULL
                            CHECK (operation IN ('deposit', 'withdrawal')),

    -- Whether the operation is currently halted on this vault.
    paused      BOOLEAN     NOT NULL DEFAULT FALSE,

    -- Operator-supplied reason, surfaced to the UI so it can explain the
    -- pause honestly instead of showing a generic error.
    reason      TEXT        NOT NULL DEFAULT '',

    -- Who last changed it. Nullable because a switch may be engaged by an
    -- operator acting through a break-glass path with no user row.
    changed_by  UUID        NULL REFERENCES users(id) ON DELETE SET NULL,

    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (vault_id, operation)
);

-- The primary key already serves lookups by vault_id, so no extra index is
-- needed for the hot "is this operation paused on this vault" read.
