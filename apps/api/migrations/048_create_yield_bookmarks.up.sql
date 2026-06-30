CREATE TABLE IF NOT EXISTS yield_bookmarks (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    protocol_slug VARCHAR(128) NOT NULL,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, protocol_slug)
);

CREATE INDEX IF NOT EXISTS idx_yield_bookmarks_user_id ON yield_bookmarks(user_id);
