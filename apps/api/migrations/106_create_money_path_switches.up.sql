-- Global pause switch for the money path (nester#1120).
--
-- Incident response, not configuration: when something goes wrong after
-- launch the team needs deposits or withdrawals stopped in seconds, without
-- editing code and redeploying.
--
-- Persisted rather than held in memory so the pause survives a restart. An
-- in-memory flag would silently release itself the moment the process
-- recycled — during an incident, which is exactly when processes recycle.
--
-- Deposits and withdrawals are separate rows so they can be halted
-- independently: the common case is stopping new money entering while still
-- letting users take theirs out.
CREATE TABLE IF NOT EXISTS money_path_switches (
    -- The operation this switch governs. A small fixed set, seeded below;
    -- the CHECK keeps a typo from creating a switch nothing enforces.
    operation   TEXT        PRIMARY KEY
                            CHECK (operation IN ('deposit', 'withdrawal')),

    -- Whether the operation is currently halted.
    paused      BOOLEAN     NOT NULL DEFAULT FALSE,

    -- Operator-supplied reason, surfaced to the UI so it can explain the
    -- pause honestly instead of showing a generic error.
    reason      TEXT        NOT NULL DEFAULT '',

    -- Who last changed it. Nullable because a switch may be engaged by an
    -- operator acting through a break-glass path with no user row.
    changed_by  UUID        NULL REFERENCES users(id) ON DELETE SET NULL,

    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Both switches exist from the start, released. The service reads a row per
-- operation and fails closed if one is missing, so seeding here is what keeps
-- a fresh database from refusing every deposit.
INSERT INTO money_path_switches (operation, paused, reason)
VALUES ('deposit', FALSE, ''),
       ('withdrawal', FALSE, '')
ON CONFLICT (operation) DO NOTHING;
