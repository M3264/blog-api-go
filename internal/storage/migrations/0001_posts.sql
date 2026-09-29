CREATE TABLE IF NOT EXISTS posts (
    slug TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    body TEXT NOT NULL,
    category TEXT NOT NULL,
    author TEXT NOT NULL DEFAULT '',
    cover_image TEXT NOT NULL DEFAULT '',
    tags_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(tags_json)),
    featured INTEGER NOT NULL DEFAULT 0 CHECK (featured IN (0, 1)),
    status TEXT NOT NULL CHECK (status IN ('draft', 'published')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    published_at TEXT
);

CREATE INDEX IF NOT EXISTS posts_public_order ON posts(status, published_at DESC, created_at DESC);
CREATE INDEX IF NOT EXISTS posts_category ON posts(status, category);
CREATE INDEX IF NOT EXISTS posts_featured ON posts(status, featured);
