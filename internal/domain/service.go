package domain

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

// FeedConfig describes a single feed's matching rules.
type FeedConfig struct {
	// URI is the AT-URI of the feed generator record.
	URI string

	// Keywords are the terms to match against post text using word boundaries.
	Keywords []string

	// Langs restricts matches to posts tagged with at least one of these
	// language codes. An empty slice means no language filter.
	Langs []string
}

// feed holds the compiled matching state for a single feed.
type feed struct {
	uri     string
	pattern *regexp.Regexp
	langs   map[string]struct{} // nil means no filter
}

func newFeedURI(publisherDID, feedName string) string {
	return fmt.Sprintf("at://%s/app.bsky.feed.generator/%s", publisherDID, feedName)
}

func NewAgenticFeedConfig(publisherDID, feedName string) FeedConfig {
	feedURI := newFeedURI(publisherDID, feedName)
	return FeedConfig{
		URI:      feedURI,
		Keywords: []string{"agentic", "agentic engineering", "agentic ai", "llm agents", "multi-agent", "llm benchmarks", "ai workflows", "llm orchestration", "context window", "claude", "claude opus", "claude sonnet", "claude haiku", "gpt-", "codex", "composer-1", "gemini", "hugging face", "opencode", "meta llama"},
		Langs:    []string{"en"},
	}
}

// FeedService is the core domain service. It owns the business logic for
// matching incoming posts against feed rules, persisting matched posts, and
// serving feed skeletons.
type FeedService struct {
	feeds   map[string]*feed // keyed by feed URI
	repo    PostRepository
	cursors CursorRepository
	logger  *slog.Logger
}

// NewFeedService creates a FeedService with the given feed configurations.
func NewFeedService(configs []FeedConfig, repo PostRepository, cursors CursorRepository, logger *slog.Logger) (*FeedService, error) {
	feeds := make(map[string]*feed, len(configs))

	for _, cfg := range configs {
		if len(cfg.Keywords) == 0 {
			return nil, fmt.Errorf("feed %s: at least one keyword is required", cfg.URI)
		}

		escaped := make([]string, len(cfg.Keywords))
		for i, kw := range cfg.Keywords {
			escaped[i] = regexp.QuoteMeta(kw)
		}

		expr := `(?i)\b(?:` + strings.Join(escaped, "|") + `)\b`
		pattern, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("feed %s: compile keyword pattern: %w", cfg.URI, err)
		}

		f := &feed{
			uri:     cfg.URI,
			pattern: pattern,
		}

		if len(cfg.Langs) > 0 {
			f.langs = make(map[string]struct{}, len(cfg.Langs))
			for _, l := range cfg.Langs {
				f.langs[l] = struct{}{}
			}
		}

		feeds[cfg.URI] = f
	}

	return &FeedService{
		feeds:   feeds,
		repo:    repo,
		cursors: cursors,
		logger:  logger,
	}, nil
}

// FeedURIs returns the AT-URIs of all registered feeds.
func (s *FeedService) FeedURIs() []string {
	uris := make([]string, 0, len(s.feeds))
	for uri := range s.feeds {
		uris = append(uris, uri)
	}
	return uris
}

// ProcessNewPost checks an incoming post against all feed rules. If any feed
// matches, the post is persisted. Returns true if the post was saved.
func (s *FeedService) ProcessNewPost(ctx context.Context, incoming *IncomingPost) (bool, error) {
	if !s.matchesAnyFeed(incoming) {
		return false, nil
	}

	post := &Post{
		URI:       incoming.URI,
		CID:       incoming.CID,
		IndexedAt: time.Now().UTC(),
	}
	if err := s.repo.CreatePost(ctx, post); err != nil {
		return false, fmt.Errorf("create post: %w", err)
	}
	return true, nil
}

// ProcessDeletePost removes a post by URI.
func (s *FeedService) ProcessDeletePost(ctx context.Context, uri string) error {
	return s.repo.DeletePost(ctx, uri)
}

// GetCursor retrieves the last-processed firehose cursor for the given service.
func (s *FeedService) GetCursor(ctx context.Context, service string) (int64, error) {
	return s.cursors.GetCursor(ctx, service)
}

// UpdateCursor persists the firehose cursor for the given service.
func (s *FeedService) UpdateCursor(ctx context.Context, service string, cursor int64) error {
	return s.cursors.UpdateCursor(ctx, service, cursor)
}

// GetFeedSkeleton returns a page of the feed skeleton for the given feed URI.
func (s *FeedService) GetFeedSkeleton(ctx context.Context, feedURI string, limit int, cursor string) (*FeedSkeleton, error) {
	s.logger.Debug("GetFeedSkeleton called", "feedURI", feedURI, "limit", limit, "cursor", cursor)

	if _, ok := s.feeds[feedURI]; !ok {
		s.logger.Error("unknown feed requested", "feedURI", feedURI, "registered_feeds", s.FeedURIs())
		return nil, fmt.Errorf("unknown feed: %s", feedURI)
	}

	s.logger.Debug("feed validated, querying repository", "feedURI", feedURI)

	posts, nextCursor, err := s.repo.GetFeedPosts(ctx, limit, cursor)
	if err != nil {
		s.logger.Error("repository query failed", "feedURI", feedURI, "limit", limit, "cursor", cursor, "error", err)
		return nil, fmt.Errorf("get feed posts: %w", err)
	}

	s.logger.Debug("repository query succeeded", "posts_count", len(posts), "next_cursor", nextCursor)

	skeleton := &FeedSkeleton{
		Cursor: nextCursor,
		Posts:  make([]SkeletonPost, len(posts)),
	}
	for i, p := range posts {
		skeleton.Posts[i] = SkeletonPost{Post: p.URI}
	}
	return skeleton, nil
}

// StartCleanupJob runs a background loop that removes posts older than maxAge
// and caps the total at maxRows. It runs immediately on start and then repeats
// at the given interval. It blocks until ctx is cancelled.
func (s *FeedService) StartCleanupJob(ctx context.Context, interval time.Duration, maxAge time.Duration, maxRows int) {
	s.runCleanup(ctx, maxAge, maxRows)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runCleanup(ctx, maxAge, maxRows)
		}
	}
}

func (s *FeedService) runCleanup(ctx context.Context, maxAge time.Duration, maxRows int) {
	deleted, err := s.repo.DeleteOldPosts(ctx, maxAge, maxRows)
	if err != nil {
		s.logger.Error("post cleanup failed", "error", err)
	} else if deleted > 0 {
		s.logger.Info("post cleanup complete", "deleted", deleted)
	}
}

// matchesAnyFeed returns true if the incoming post matches at least one feed.
func (s *FeedService) matchesAnyFeed(incoming *IncomingPost) bool {
	for _, f := range s.feeds {
		if matchesFeed(f, incoming) {
			return true
		}
	}
	return false
}

func matchesFeed(f *feed, incoming *IncomingPost) bool {
	if f.langs != nil {
		matched := false
		for _, l := range incoming.Langs {
			if _, ok := f.langs[l]; ok {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return f.pattern.MatchString(incoming.Text)
}
