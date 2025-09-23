CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE IF NOT EXISTS profiles (
  user_id     UUID PRIMARY KEY,
  username    TEXT NOT NULL,
  age         INT  NOT NULL DEFAULT 0,
  country     TEXT NOT NULL DEFAULT '',
  gender      SMALLINT NOT NULL DEFAULT 0,
  avatar_key  TEXT NOT NULL DEFAULT '',
  avatar_url  TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);