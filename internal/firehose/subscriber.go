package firehose

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/blackmichael/bluesky-feeds/internal/domain"
	"github.com/gorilla/websocket"
)

const (
	cursorServiceName  = "jetstream"
	cursorSaveInterval = 5 * time.Second
)

// wantedCollections is the set of AT Proto collection NSIDs this subscriber
// requests from Jetstream. Only post events are needed for feed matching.
var wantedCollections = []string{
	"app.bsky.feed.post",
}

// Subscriber connects to the Jetstream firehose and processes events.
type Subscriber struct {
	url         string
	feedService *domain.FeedService
	logger      *slog.Logger
}

// NewSubscriber creates a new firehose subscriber.
func NewSubscriber(
	firehoseURL string,
	feedService *domain.FeedService,
	logger *slog.Logger,
) *Subscriber {
	return &Subscriber{
		url:         firehoseURL,
		feedService: feedService,
		logger:      logger,
	}
}

// Start connects to the firehose and processes events until the context is
// cancelled. It automatically reconnects on transient errors.
func (s *Subscriber) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := s.subscribe(ctx); err != nil {
				s.logger.Error("firehose connection error, reconnecting", "error", err)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(5 * time.Second):
					// backoff before reconnecting
				}
			}
		}
	}
}

func (s *Subscriber) buildURL(cursor int64) string {
	u, _ := url.Parse(s.url)
	q := u.Query()
	for _, c := range wantedCollections {
		q.Add("wantedCollections", c)
	}
	if cursor > 0 {
		q.Set("cursor", fmt.Sprintf("%d", cursor))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *Subscriber) subscribe(ctx context.Context) error {
	cursor, err := s.feedService.GetCursor(ctx, cursorServiceName)
	if err != nil {
		s.logger.Warn("failed to load cursor, starting from live", "error", err)
	}

	wsURL := s.buildURL(cursor)
	s.logger.Info("connecting to firehose", "url", wsURL)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial firehose: %w", err)
	}
	defer conn.Close()

	s.logger.Info("connected to firehose")

	lastCursorSave := time.Now()
	var latestCursor int64
	var eventsReceived, commitsReceived, postsMatched int64
	lastStatsLog := time.Now()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read message: %w", err)
		}

		event, err := parseEvent(message)
		if err != nil {
			s.logger.Error("failed to parse event", "error", err)
			continue
		}

		eventsReceived++
		latestCursor = event.TimeUS

		if event.Kind == "commit" && event.Commit != nil {
			commitsReceived++
			if matched, err := s.handleCommit(ctx, event); err != nil {
				s.logger.Error("failed to handle commit", "error", err)
			} else if matched {
				postsMatched++
			}
		}

		// Log stats every 30 seconds
		if time.Since(lastStatsLog) >= 30*time.Second {
			s.logger.Info("firehose stats",
				"events_received", eventsReceived,
				"commits_received", commitsReceived,
				"posts_matched", postsMatched,
			)
			lastStatsLog = time.Now()
		}

		// Periodically save cursor
		if time.Since(lastCursorSave) >= cursorSaveInterval {
			if err := s.feedService.UpdateCursor(ctx, cursorServiceName, latestCursor); err != nil {
				s.logger.Error("failed to save cursor", "error", err)
			} else {
				lastCursorSave = time.Now()
			}
		}
	}
}

func (s *Subscriber) handleCommit(ctx context.Context, event *jetstreamEvent) (matched bool, err error) {
	commit := event.Commit
	if commit.Collection != "app.bsky.feed.post" {
		return false, nil
	}

	uri := fmt.Sprintf("at://%s/%s/%s", event.DID, commit.Collection, commit.RKey)

	switch commit.Operation {
	case "create":
		if commit.Record == nil {
			return false, nil
		}

		incoming := &domain.IncomingPost{
			URI:       uri,
			CID:       commit.CID,
			AuthorDID: event.DID,
			Text:      commit.Record.Text,
			Langs:     commit.Record.Langs,
		}

		matched, err := s.feedService.ProcessNewPost(ctx, incoming)
		if err != nil {
			return false, err
		}

		if matched {
			s.logger.Info("matched post",
				"uri", uri,
				"text_preview", truncate(incoming.Text, 100),
			)
		}

		return matched, nil

	case "delete":
		return false, s.feedService.ProcessDeletePost(ctx, uri)

	default:
		return false, nil
	}
}

// truncate returns the first n characters of s, appending "..." if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func parseEvent(data []byte) (*jetstreamEvent, error) {
	var raw struct {
		DID    string          `json:"did"`
		TimeUS int64           `json:"time_us"`
		Kind   string          `json:"kind"`
		Commit json.RawMessage `json:"commit,omitempty"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal event: %w", err)
	}

	event := &jetstreamEvent{
		DID:    raw.DID,
		TimeUS: raw.TimeUS,
		Kind:   raw.Kind,
	}

	if raw.Kind == "commit" && len(raw.Commit) > 0 {
		var rc struct {
			Rev        string          `json:"rev"`
			Operation  string          `json:"operation"`
			Collection string          `json:"collection"`
			RKey       string          `json:"rkey"`
			Record     json.RawMessage `json:"record,omitempty"`
			CID        string          `json:"cid"`
		}
		if err := json.Unmarshal(raw.Commit, &rc); err != nil {
			return nil, fmt.Errorf("unmarshal commit: %w", err)
		}

		commit := &jetstreamCommit{
			Rev:        rc.Rev,
			Operation:  rc.Operation,
			Collection: rc.Collection,
			RKey:       rc.RKey,
			CID:        rc.CID,
		}

		if len(rc.Record) > 0 && strings.HasPrefix(rc.Collection, "app.bsky.feed.post") {
			var record postRecord
			if err := json.Unmarshal(rc.Record, &record); err != nil {
				return nil, fmt.Errorf("unmarshal post record: %w", err)
			}
			commit.Record = &record
		}

		event.Commit = commit
	}

	return event, nil
}
