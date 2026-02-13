package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/blackmichael/bluesky-feeds/internal/domain"
	_ "github.com/lib/pq"
)

// Repository implements domain.PostRepository and domain.CursorRepository
// using PostgreSQL.
type Repository struct {
	db *sql.DB
}

// NewRepository connects to PostgreSQL at the given URL, verifies the
// connection, and returns a new Repository. The caller should call Close
// when the repository is no longer needed.
func NewRepository(databaseURL string) (*Repository, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &Repository{db: db}, nil
}

// Close closes the underlying database connection.
func (r *Repository) Close() error {
	return r.db.Close()
}

// CreatePost inserts a new post.
func (r *Repository) CreatePost(ctx context.Context, post *domain.Post) error {
	query := `
		INSERT INTO posts (uri, cid, indexed_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (uri) DO NOTHING`

	_, err := r.db.ExecContext(ctx, query,
		post.URI,
		post.CID,
		post.IndexedAt,
	)
	return err
}

// DeletePost removes a post by URI.
func (r *Repository) DeletePost(ctx context.Context, uri string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM posts WHERE uri = $1`, uri)
	return err
}

// GetFeedPosts retrieves posts paginated by cursor.
// The cursor format is "indexedAt::cid" (unix millis::cid).
func (r *Repository) GetFeedPosts(ctx context.Context, limit int, cursor string) ([]domain.Post, string, error) {
	var (
		rows *sql.Rows
		err  error
	)

	if cursor != "" {
		cursorTime, cursorCID, parseErr := parseCursor(cursor)
		if parseErr != nil {
			return nil, "", fmt.Errorf("invalid cursor '%s': %w", cursor, parseErr)
		}

		rows, err = r.db.QueryContext(ctx, `
			SELECT uri, cid, indexed_at
			FROM posts
			WHERE (indexed_at, cid) < ($1, $2)
			ORDER BY indexed_at DESC, cid DESC
			LIMIT $3`,
			cursorTime, cursorCID, limit,
		)
		if err != nil {
			return nil, "", fmt.Errorf("query posts with cursor (time=%v, cid=%s, limit=%d): %w", cursorTime, cursorCID, limit, err)
		}
	} else {
		rows, err = r.db.QueryContext(ctx, `
			SELECT uri, cid, indexed_at
			FROM posts
			ORDER BY indexed_at DESC, cid DESC
			LIMIT $1`,
			limit,
		)
		if err != nil {
			return nil, "", fmt.Errorf("query posts without cursor (limit=%d): %w", limit, err)
		}
	}
	defer rows.Close()

	var posts []domain.Post
	for rows.Next() {
		var p domain.Post
		err := rows.Scan(
			&p.URI,
			&p.CID,
			&p.IndexedAt,
		)
		if err != nil {
			return nil, "", fmt.Errorf("scan post: %w", err)
		}
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

// DeleteOldPosts removes posts older than maxAge and any excess rows beyond
// maxRows, keeping the most recent posts. Returns the total number of rows deleted.
func (r *Repository) DeleteOldPosts(ctx context.Context, maxAge time.Duration, maxRows int) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete posts older than maxAge
	res, err := tx.ExecContext(ctx,
		`DELETE FROM posts WHERE indexed_at < $1`,
		time.Now().UTC().Add(-maxAge),
	)
	if err != nil {
		return 0, fmt.Errorf("delete expired posts: %w", err)
	}
	ttlDeleted, _ := res.RowsAffected()

	// Delete excess rows beyond maxRows, keeping the most recent
	res, err = tx.ExecContext(ctx, `
		DELETE FROM posts WHERE uri IN (
			SELECT uri FROM posts
			ORDER BY indexed_at DESC, cid DESC
			OFFSET $1
		)`, maxRows,
	)
	if err != nil {
		return 0, fmt.Errorf("delete excess posts: %w", err)
	}
	capDeleted, _ := res.RowsAffected()

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}

	return ttlDeleted + capDeleted, nil
}

// GetCursor retrieves the saved firehose cursor for a service.
func (r *Repository) GetCursor(ctx context.Context, service string) (int64, error) {
	var cursor int64
	err := r.db.QueryRowContext(ctx,
		`SELECT cursor_value FROM cursors WHERE service = $1`, service,
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
		VALUES ($1, $2, $3)
		ON CONFLICT (service) DO UPDATE SET cursor_value = $2, updated_at = $3`,
		service, cursor, time.Now().UTC(),
	)
	return err
}

func parseCursor(cursor string) (time.Time, string, error) {
	parts := strings.SplitN(cursor, "::", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("cursor must be in format 'timestamp::cid'")
	}
	millis, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid timestamp in cursor: %w", err)
	}
	return time.UnixMilli(millis), parts[1], nil
}
