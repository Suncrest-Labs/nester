CREATE TABLE IF NOT EXISTS user_roles (
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       VARCHAR(50) NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    granted_by UUID        REFERENCES users(id),
    PRIMARY KEY (user_id, role)
);
