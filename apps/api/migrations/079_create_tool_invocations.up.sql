CREATE TABLE IF NOT EXISTS tool_invocations (
    id UUID PRIMARY KEY,
    user_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    conversation_id TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    arguments JSONB NOT NULL,
    consequential BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL,
    result JSONB,
    error_message TEXT,
    prev_hash TEXT NOT NULL,
    entry_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tool_invocations_user_id_created_at ON tool_invocations (user_id, created_at DESC);
