CREATE TABLE IF NOT EXISTS user_watchlist (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pool_id    VARCHAR(255) NOT NULL,
    pool_symbol  VARCHAR(100),
    pool_project VARCHAR(100),
    pool_chain   VARCHAR(50),
    apy_at_save  NUMERIC(10, 4),
    tvl_usd      NUMERIC(20, 2),
    added_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, pool_id)
);

CREATE INDEX IF NOT EXISTS idx_user_watchlist_user_id ON user_watchlist(user_id);
