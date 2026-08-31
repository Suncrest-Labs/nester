-- Widen vault balance columns so Soroban i128 stroop amounts round-trip exactly (PRD B-11, issue #1051).
--
-- NUMERIC(20,8) allows only 12 integer digits, so any value >= 10^12 raised
-- "numeric field overflow" and the event was rejected outright. Soroban token
-- amounts are i128 stroops and routinely exceed that: a 1e18 stroop deposit is
-- an ordinary vault deposit, not an edge case.
--
-- NUMERIC(48,8) holds 40 integer digits, which covers the full i128 range
-- (max ~1.7e38) with headroom. Widening precision is a metadata-only change in
-- PostgreSQL for these columns and preserves every stored value exactly.

ALTER TABLE vaults
    ALTER COLUMN total_deposited TYPE NUMERIC(48, 8),
    ALTER COLUMN current_balance TYPE NUMERIC(48, 8),
    ALTER COLUMN yield_earned    TYPE NUMERIC(48, 8),
    ALTER COLUMN fees_paid       TYPE NUMERIC(48, 8);
