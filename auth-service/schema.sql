CREATE EXTENSION IF NOT EXISTS pgcrypto;

DROP TABLE IF EXISTS users;
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DROP TABLE IF EXISTS refresh_tokens;
CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid,
    token_hash TEXT UNIQUE NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX idx_refresh_user_id ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_expires_at ON refresh_tokens(expires_at);
CREATE INDEX idx_refresh_active_user ON refresh_tokens(user_id) WHERE revoked = false;