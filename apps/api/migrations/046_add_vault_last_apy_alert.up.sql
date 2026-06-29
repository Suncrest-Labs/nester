ALTER TABLE vaults
    ADD COLUMN IF NOT EXISTS last_apy_alert_sent_at TIMESTAMPTZ;
