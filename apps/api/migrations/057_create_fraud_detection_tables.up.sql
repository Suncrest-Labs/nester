CREATE TABLE user_baselines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    avg_transaction_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    stddev_transaction_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    max_transaction_amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    avg_daily_transactions FLOAT NOT NULL DEFAULT 0,
    avg_hourly_transactions FLOAT NOT NULL DEFAULT 0,
    known_destination_count INT NOT NULL DEFAULT 0,
    known_device_count INT NOT NULL DEFAULT 0,
    typical_hour_start INT NOT NULL DEFAULT 0,
    typical_hour_end INT NOT NULL DEFAULT 23,
    transaction_count INT NOT NULL DEFAULT 0,
    last_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id)
);

CREATE INDEX IF NOT EXISTS idx_user_baselines_user_id ON user_baselines(user_id);

CREATE TABLE user_known_destinations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    destination TEXT NOT NULL,
    label TEXT,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    occurrence_count INT NOT NULL DEFAULT 1,
    UNIQUE(user_id, destination)
);

CREATE INDEX IF NOT EXISTS idx_user_known_destinations_user_id ON user_known_destinations(user_id);

CREATE TABLE user_known_devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_fingerprint TEXT NOT NULL,
    label TEXT,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, device_fingerprint)
);

CREATE INDEX IF NOT EXISTS idx_user_known_devices_user_id ON user_known_devices(user_id);

CREATE TYPE fraud_severity AS ENUM ('low', 'medium', 'high', 'critical');
CREATE TYPE fraud_action_type AS ENUM ('log', 'step_up_auth', 'hold', 'block');
CREATE TYPE fraud_flag_status AS ENUM ('open', 'cleared', 'confirmed', 'auto_cleared');

CREATE TABLE fraud_flags (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    transaction_id UUID REFERENCES transactions(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    severity fraud_severity NOT NULL DEFAULT 'low',
    status fraud_flag_status NOT NULL DEFAULT 'open',
    signals JSONB NOT NULL DEFAULT '[]',
    risk_score FLOAT NOT NULL DEFAULT 0,
    explanation TEXT,
    user_notified BOOLEAN NOT NULL DEFAULT FALSE,
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    cleared_by_user BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_fraud_flags_user_id ON fraud_flags(user_id);
CREATE INDEX IF NOT EXISTS idx_fraud_flags_status ON fraud_flags(status);
CREATE INDEX IF NOT EXISTS idx_fraud_flags_severity ON fraud_flags(severity);
CREATE INDEX IF NOT EXISTS idx_fraud_flags_created_at ON fraud_flags(created_at);

CREATE TABLE fraud_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    flag_id UUID NOT NULL REFERENCES fraud_flags(id) ON DELETE CASCADE,
    action fraud_action_type NOT NULL,
    reason TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_fraud_actions_flag_id ON fraud_actions(flag_id);

CREATE TABLE auth_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    ip_address TEXT,
    device_fingerprint TEXT,
    location_lat DOUBLE PRECISION,
    location_lon DOUBLE PRECISION,
    location_city TEXT,
    location_country TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_auth_events_user_id ON auth_events(user_id);
CREATE INDEX IF NOT EXISTS idx_auth_events_created_at ON auth_events(created_at);
CREATE INDEX IF NOT EXISTS idx_auth_events_user_created ON auth_events(user_id, created_at DESC);

CREATE TABLE fraud_metrics (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    metric_name TEXT NOT NULL,
    metric_value FLOAT NOT NULL,
    tags JSONB NOT NULL DEFAULT '{}',
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_fraud_metrics_name ON fraud_metrics(metric_name);
CREATE INDEX IF NOT EXISTS idx_fraud_metrics_recorded_at ON fraud_metrics(recorded_at);
