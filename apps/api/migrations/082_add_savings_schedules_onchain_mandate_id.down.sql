-- Remove on-chain mandate ID column from savings schedules

DROP INDEX IF EXISTS idx_savings_schedules_onchain_mandate_id;
ALTER TABLE savings_schedules DROP COLUMN IF EXISTS onchain_mandate_id;