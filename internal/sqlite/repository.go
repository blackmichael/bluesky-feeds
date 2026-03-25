package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/blackmichael/bluesky-feeds/internal/domain"
)

// Repository implements domain.PostRepository and domain.CursorRepository
// using SQLite.
type Repository struct {
	db *sql.DB
}

// NewRepository opens the SQLite database at path, applies the schema,
// and returns a new Repository.
func NewRepository(path string) (*Repository, error) {
	db, err := Open(path)
	if err != nil {
		return nil, err
	}
	return &Repository{db: db}, nil
}

// Close closes the underlying database connection.
func (r *Repository) Close() error {
	return r.db.Close()
}

// CreatePost inserts a post row for each matched feed URI.
func (r *Repository) CreatePost(ctx context.Context, post *domain.Post, feedURIs []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO posts (uri, cid, feed_uri, indexed_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (uri, feed_uri) DO NOTHING`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	millis := post.IndexedAt.UnixMilli()
	for _, feedURI := range feedURIs {
		if _, err := stmt.ExecContext(ctx, post.URI, post.CID, feedURI, millis); err != nil {
			return fmt.Errorf("insert post for feed %s: %w", feedURI, err)
		}
	}

	return tx.Commit()
}

// DeletePost removes all rows for a post URI across all feeds.
func (r *Repository) DeletePost(ctx context.Context, uri string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM posts WHERE uri = ?`, uri)
	return err
}

// GetFeedPosts retrieves posts for a specific feed, paginated by cursor.
// Cursor format: "indexedAtMillis::cid".
func (r *Repository) GetFeedPosts(ctx context.Context, feedURI string, limit int, cursor string) ([]domain.Post, string, error) {
	var (
		rows *sql.Rows
		err  error
	)

	if cursor != "" {
		cursorMillis, cursorCID, parseErr := parseCursor(cursor)
		if parseErr != nil {
			return nil, "", fmt.Errorf("invalid cursor %q: %w", cursor, parseErr)
		}

		rows, err = r.db.QueryContext(ctx, `
			SELECT uri, cid, indexed_at
			FROM posts
			WHERE feed_uri = ?
			  AND (indexed_at, cid) < (?, ?)
			ORDER BY indexed_at DESC, cid DESC
			LIMIT ?`,
			feedURI, cursorMillis, cursorCID, limit,
		)
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT uri, cid, indexed_at
			FROM posts
			WHERE feed_uri = ?
			ORDER BY indexed_at DESC, cid DESC
			LIMIT ?`,
			feedURI, limit,
		)
	}
	if err != nil {
		return nil, "", fmt.Errorf("query feed posts: %w", err)
	}
	defer rows.Close()

	var posts []domain.Post
	for rows.Next() {
		var (
			p      domain.Post
			millis int64
		)
		if err := rows.Scan(&p.URI, &p.CID, &millis); err != nil {
			return nil, "", fmt.Errorf("scan post: %w", err)
		}
		p.IndexedAt = time.UnixMilli(millis).UTC()
		posts = append(posts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate posts: %w", err)
	}

	var nextCursor string
	if len(posts) == limit {
		last := posts[len(posts)-1]
		nextCursor = fmt.Sprintf("%d::%s", last.IndexedAt.UnixMilli(), last.CID)
	}

	return posts, nextCursor, nil
}

// DeleteOldPosts removes posts for a specific feed older than maxAge and
// caps the feed at maxRows, keeping the most recent. Returns total rows deleted.
func (r *Repository) DeleteOldPosts(ctx context.Context, feedURI string, maxAge time.Duration, maxRows int) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	cutoffMillis := time.Now().UTC().Add(-maxAge).UnixMilli()

	// Delete posts older than maxAge for this feed.
	res, err := tx.ExecContext(ctx,
		`DELETE FROM posts WHERE feed_uri = ? AND indexed_at < ?`,
		feedURI, cutoffMillis,
	)
	if err != nil {
		return 0, fmt.Errorf("delete expired posts: %w", err)
	}
	ttlDeleted, _ := res.RowsAffected()

	// Cap at maxRows by deleting excess, keeping the most recent.
	res, err = tx.ExecContext(ctx, `
		DELETE FROM posts
		WHERE feed_uri = ?
		  AND rowid IN (
			SELECT rowid FROM posts
			WHERE feed_uri = ?
			ORDER BY indexed_at DESC, cid DESC
			LIMIT -1 OFFSET ?
		  )`,
		feedURI, feedURI, maxRows,
	)
	if err != nil {
		return 0, fmt.Errorf("delete excess posts: %w", err)
	}
	capDeleted, _ := res.RowsAffected()

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	return ttlDeleted + capDeleted, nil
}

// GetCursor retrieves the saved firehose cursor for a service.
func (r *Repository) GetCursor(ctx context.Context, service string) (int64, error) {
	var cursor int64
	err := r.db.QueryRowContext(ctx,
		`SELECT cursor_value FROM cursors WHERE service = ?`, service,
	).Scan(&cursor)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return cursor, err
}

// UpdateCursor upserts the firehose cursor for a service.
func (r *Repository) UpdateCursor(ctx context.Context, service string, cursor int64) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO cursors (service, cursor_value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT (service) DO UPDATE SET
			cursor_value = excluded.cursor_value,
			updated_at = excluded.updated_at`,
		service, cursor, time.Now().UTC().UnixMilli(),
	)
	return err
}

func parseCursor(cursor string) (int64, string, error) {
	parts := strings.SplitN(cursor, "::", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("cursor must be in format 'timestamp::cid'")
	}
	millis, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid timestamp in cursor: %w", err)
	}
	return millis, parts[1], nil
}
