package domain

import (
	"context"
	"time"
)

// PostRepository defines persistence operations for indexed posts.
type PostRepository interface {
	// CreatePost inserts a new post into the store, associating it with the
	// given feed URIs. Each feed gets its own row.
	CreatePost(ctx context.Context, post *Post, feedURIs []string) error

	// DeletePost removes a post by its AT-URI across all feeds.
	DeletePost(ctx context.Context, uri string) error

	// DeleteOldPosts removes posts for a specific feed older than maxAge and
	// caps the feed at maxRows, keeping the most recent. Returns rows deleted.
	DeleteOldPosts(ctx context.Context, feedURI string, maxAge time.Duration, maxRows int) (int64, error)

	// GetFeedPosts retrieves posts for the given feed URI, ordered by
	// indexedAt descending. The cursor is opaque and implementation-defined.
	// Returns posts and the next cursor (empty string if no more results).
	GetFeedPosts(ctx context.Context, feedURI string, limit int, cursor string) ([]Post, string, error)
}

// CursorRepository defines persistence operations for firehose cursors.
type CursorRepository interface {
	// GetCursor retrieves the last-processed firehose cursor for the given
	// service name. Returns 0 if no cursor has been saved.
	GetCursor(ctx context.Context, service string) (int64, error)

	// UpdateCursor persists the firehose cursor so we can resume on restart.
	UpdateCursor(ctx context.Context, service string, cursor int64) error
}
