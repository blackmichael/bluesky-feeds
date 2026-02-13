package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all configuration for the application.
type Config struct {
	// Hostname is the public hostname where this service is reachable (used for did:web).
	Hostname string

	// Port is the HTTP server port.
	Port int

	// PublisherDID is the DID of the account that published the feed generator records.
	PublisherDID string

	// DatabaseURL is the Postgres connection string.
	DatabaseURL string

	// FirehoseURL is the Jetstream WebSocket endpoint.
	FirehoseURL string
}

// ServiceDID returns the did:web for this feed generator based on the hostname.
func (c *Config) ServiceDID() string {
	return "did:web:" + c.Hostname
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	port := 3000
	if p := os.Getenv("PORT"); p != "" {
		var err error
		port, err = strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid PORT: %w", err)
		}
	}

	hostname := os.Getenv("FEEDGEN_HOSTNAME")
	if hostname == "" {
		hostname = "localhost"
	}

	publisherDID := os.Getenv("FEEDGEN_PUBLISHER_DID")
	if publisherDID == "" {
		return nil, fmt.Errorf("FEEDGEN_PUBLISHER_DID is required")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/bluesky_feeds?sslmode=disable"
	}

	firehoseURL := os.Getenv("FEEDGEN_FIREHOSE_URL")
	if firehoseURL == "" {
		firehoseURL = "wss://jetstream1.us-east.bsky.network/subscribe"
	}

	return &Config{
		Hostname:     hostname,
		Port:         port,
		PublisherDID: publisherDID,
		DatabaseURL:  dbURL,
		FirehoseURL:  firehoseURL,
	}, nil
}
