package domain

import (
	"context"
	"time"
)

// PostRepository defines persistence operations for indexed posts.
type PostRepository interface {
	// CreatePost inserts a new post into the store.
	CreatePost(ctx context.Context, post *Post) error

	// DeletePost removes a post by its AT-URI.
	DeletePost(ctx context.Context, uri string) error

	// DeleteOldPosts removes posts older than maxAge and any excess rows beyond
	// maxRows, keeping the most recent posts. Returns the number of rows deleted.
	DeleteOldPosts(ctx context.Context, maxAge time.Duration, maxRows int) (int64, error)

	// GetFeedPosts retrieves posts ordered by indexedAt descending. The cursor
	// is opaque and implementation-defined. Returns posts and the next cursor
	// (empty string if no more results).
	GetFeedPosts(ctx context.Context, limit int, cursor string) ([]Post, string, error)
}

// CursorRepository defines persistence operations for firehose cursors.
type CursorRepository interface {
	// GetCursor retrieves the last-processed firehose cursor for the given
	// service name. Returns 0 if no cursor has been saved.
	GetCursor(ctx context.Context, service string) (int64, error)

	// UpdateCursor persists the firehose cursor so we can resume on restart.
	UpdateCursor(ctx context.Context, service string, cursor int64) error
}
