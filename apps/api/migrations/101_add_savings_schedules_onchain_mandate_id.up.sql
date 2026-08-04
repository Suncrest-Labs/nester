-- Add on-chain mandate ID to savings schedules for issue #808
-- This links off-chain savings schedules with on-chain recurring deposit mandates

ALTER TABLE savings_schedules 
ADD COLUMN onchain_mandate_id BIGINT NULL;

-- Create index for efficient lookups by mandate ID
CREATE INDEX IF NOT EXISTS idx_savings_schedules_onchain_mandate_id 
ON savings_schedules(onchain_mandate_id) 
WHERE onchain_mandate_id IS NOT NULL;

-- Add comment explaining the column
COMMENT ON COLUMN savings_schedules.onchain_mandate_id IS 'Links to on-chain recurring deposit mandate ID when using blockchain-based automation';