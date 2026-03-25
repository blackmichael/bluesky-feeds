CREATE TABLE posts (
    uri        TEXT    NOT NULL,
    cid        TEXT    NOT NULL,
    feed_uri   TEXT    NOT NULL,
    indexed_at INTEGER NOT NULL,
    PRIMARY KEY (uri, feed_uri)
);

CREATE INDEX idx_posts_feed_indexed
    ON posts (feed_uri, indexed_at DESC, cid DESC);

CREATE TABLE cursors (
    service      TEXT    PRIMARY KEY,
    cursor_value INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
