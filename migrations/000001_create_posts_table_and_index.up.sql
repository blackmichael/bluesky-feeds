CREATE TABLE posts (
	uri TEXT PRIMARY KEY,
	cid TEXT NOT NULL,
	indexed_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_posts_indexed_at ON posts (indexed_at DESC, cid DESC);