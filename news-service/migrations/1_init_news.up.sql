CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE IF NOT EXISTS news (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title            text        NOT NULL,
    category         text        NOT NULL DEFAULT '',
    short_description text       NOT NULL DEFAULT '',
    long_description  text       NOT NULL DEFAULT '',
    link             CITEXT UNIQUE NOT NULL,
    image_url        text        NOT NULL DEFAULT '',
    published_at     TIMESTAMPTZ NOT NULL DEFAULT now(),    
    fetched_at       TIMESTAMPTZ NOT NULL DEFAULT now()  
);

CREATE INDEX IF NOT EXISTS ix_news_published_id_desc
    ON news (published_at DESC, id DESC);