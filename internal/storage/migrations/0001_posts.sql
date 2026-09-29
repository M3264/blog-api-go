CREATE TABLE IF NOT EXISTS posts (
    slug TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    body TEXT NOT NULL,
    category TEXT NOT NULL,
    author TEXT NOT NULL DEFAULT '',
    cover_image TEXT NOT NULL DEFAULT '',
    tags_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    featured BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL CHECK (status IN ('draft', 'published')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    published_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS posts_public_order ON posts(status, published_at DESC, created_at DESC);
CREATE INDEX IF NOT EXISTS posts_category ON posts(status, lower(category));
CREATE INDEX IF NOT EXISTS posts_featured ON posts(status, featured);
