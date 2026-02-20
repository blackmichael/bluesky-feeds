package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/blackmichael/bluesky-feeds/internal/config"
	"github.com/blackmichael/bluesky-feeds/internal/domain"
	"github.com/blackmichael/bluesky-feeds/internal/firehose"
	"github.com/blackmichael/bluesky-feeds/internal/httpserver"
	"github.com/blackmichael/bluesky-feeds/internal/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Set up repository (implements both PostRepository and CursorRepository)
	repo, err := postgres.NewRepository(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create repository: %w", err)
	}
	defer repo.Close()
	logger.Info("connected to database")

	// Set up feed service with feed configurations
	feedConfigs := domain.GetFeedConfigs(cfg.PublisherDID)
	feedService, err := domain.NewFeedService(feedConfigs, repo, repo, logger)
	if err != nil {
		return fmt.Errorf("create feed service: %w", err)
	}

	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start the firehose subscriber in the background
	subscriber := firehose.NewSubscriber(cfg.FirehoseURL, feedService, logger)
	go func() {
		if err := subscriber.Start(ctx); err != nil && ctx.Err() == nil {
			logger.Error("firehose subscriber exited with error", "error", err)
		}
	}()

	// Start background post cleanup
	go feedService.StartCleanupJob(ctx, time.Minute, 7*24*time.Hour, 500)

	// Start the HTTP server
	server := httpserver.NewServer(cfg, feedService, logger)
	go func() {
		if err := server.Start(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server exited with error", "error", err)
		}
	}()

	logger.Info("server started", "port", cfg.Port, "hostname", cfg.Hostname)

	// Wait for shutdown signal
	sig := <-sigCh
	logger.Info("received signal, shutting down", "signal", sig)
	cancel()

	if err := server.Shutdown(context.Background()); err != nil {
		logger.Error("error shutting down http server", "error", err)
	}

	return nil
}
