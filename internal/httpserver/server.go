package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/blackmichael/bluesky-feeds/internal/config"
	"github.com/blackmichael/bluesky-feeds/internal/domain"
)

// Server is the HTTP server that serves feed generator XRPC endpoints.
type Server struct {
	cfg         *config.Config
	feedService *domain.FeedService
	logger      *slog.Logger
	httpServer  *http.Server
}

// NewServer creates a new HTTP server with the given feed service.
func NewServer(cfg *config.Config, feedService *domain.FeedService, logger *slog.Logger) *Server {
	s := &Server{
		cfg:         cfg,
		feedService: feedService,
		logger:      logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/did.json", s.handleDIDDoc)
	mux.HandleFunc("GET /xrpc/app.bsky.feed.describeFeedGenerator", s.handleDescribeFeedGenerator)
	mux.HandleFunc("GET /xrpc/app.bsky.feed.getFeedSkeleton", s.handleGetFeedSkeleton)
	mux.HandleFunc("GET /health", s.handleHealth)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      withLogging(logger, mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Start begins listening for HTTP requests. It blocks until the server is
// shut down or an error occurs.
func (s *Server) Start() error {
	s.logger.Info("starting HTTP server", "addr", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDIDDoc(w http.ResponseWriter, _ *http.Request) {
	doc := map[string]any{
		"@context": []string{"https://www.w3.org/ns/did/v1"},
		"id":       s.cfg.ServiceDID(),
		"service": []map[string]any{
			{
				"id":              "#bsky_fg",
				"type":            "BskyFeedGenerator",
				"serviceEndpoint": fmt.Sprintf("https://%s", s.cfg.Hostname),
			},
		},
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleDescribeFeedGenerator(w http.ResponseWriter, _ *http.Request) {
	uris := s.feedService.FeedURIs()
	feeds := make([]map[string]string, 0, len(uris))
	for _, uri := range uris {
		feeds = append(feeds, map[string]string{"uri": uri})
	}

	resp := map[string]any{
		"did":   s.cfg.ServiceDID(),
		"feeds": feeds,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetFeedSkeleton(w http.ResponseWriter, r *http.Request) {
	feedURI := r.URL.Query().Get("feed")
	if feedURI == "" {
		s.logger.Warn("getFeedSkeleton called without feed parameter")
		writeError(w, http.StatusBadRequest, "InvalidRequest", "feed parameter is required")
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		parsed, err := strconv.Atoi(l)
		if err != nil || parsed < 1 || parsed > 100 {
			s.logger.Warn("invalid limit parameter", "limit", l, "error", err)
			writeError(w, http.StatusBadRequest, "InvalidRequest", "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}

	cursor := r.URL.Query().Get("cursor")

	s.logger.Info("getFeedSkeleton request", "feed", feedURI, "limit", limit, "cursor", cursor)

	skeleton, err := s.feedService.GetFeedSkeleton(r.Context(), feedURI, limit, cursor)
	if err != nil {
		s.logger.Error("failed to get feed skeleton",
			"feed", feedURI,
			"limit", limit,
			"cursor", cursor,
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "InternalError", "failed to get feed")
		return
	}

	s.logger.Info("getFeedSkeleton success", "feed", feedURI, "posts_returned", len(skeleton.Posts), "next_cursor", skeleton.Cursor)

	resp := map[string]any{
		"feed": toSkeletonResponse(skeleton.Posts),
	}
	if skeleton.Cursor != "" {
		resp["cursor"] = skeleton.Cursor
	}

	writeJSON(w, http.StatusOK, resp)
}

func toSkeletonResponse(posts []domain.SkeletonPost) []map[string]string {
	result := make([]map[string]string, len(posts))
	for i, p := range posts {
		result[i] = map[string]string{"post": p.Post}
	}
	return result
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, errType, message string) {
	writeJSON(w, status, map[string]string{
		"error":   errType,
		"message": message,
	})
}

func withLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.status,
			"duration", time.Since(start),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
