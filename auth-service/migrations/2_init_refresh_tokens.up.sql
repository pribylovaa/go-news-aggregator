CREATE TABLE IF NOT EXISTS refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash TEXT UNIQUE NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_refresh_user_id
    ON refresh_tokens(user_id);

CREATE INDEX IF NOT EXISTS idx_refresh_expires_at
    ON refresh_tokens(expires_at);

CREATE INDEX IF NOT EXISTS idx_refresh_active_user
    ON refresh_tokens(user_id) WHERE revoked = false;