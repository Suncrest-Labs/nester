-- Idempotency for ledger postings (nester#1309): a retried request (webhook
-- redelivery, client retry) must not double-post the same logical movement.
--
-- One domain event (domain_event_type + domain_event_id) posts multiple legs,
-- one per account_id, that together sum to zero (see ledger_entries table
-- comment in migration 114). The constraint therefore cannot be unique on
-- (domain_event_type, domain_event_id) alone -- that would reject the second
-- and later legs of the *same* valid posting. Instead it is unique per leg:
-- (domain_event_type, domain_event_id, account_id). A retried post of the
-- exact same domain event reuses the same account_id values for its legs and
-- collides on every row, so the whole duplicate posting is rejected; distinct
-- accounts within one domain event's legs remain free to insert.
--
-- The constraint only applies when domain_event_id is present: many legacy/
-- internal postings (e.g. ad-hoc rebalances) have no domain_event_id, and a
-- NULL or empty value must never be treated as a duplicate key across
-- unrelated postings. A partial unique index enforces this scoping.
CREATE UNIQUE INDEX IF NOT EXISTS uq_ledger_entries_domain_event_account
    ON ledger_entries (domain_event_type, domain_event_id, account_id)
    WHERE domain_event_id IS NOT NULL AND domain_event_id != '';
