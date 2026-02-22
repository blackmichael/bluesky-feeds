package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blackmichael/bluesky-feeds/internal/bluesky"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		handle      string
		password    string
		pds         string
		serviceDID  string
		feedRKey    string
		displayName string
		description string
		avatarPath  string
		unpublish   bool
	)

	flag.StringVar(&handle, "handle", envOrDefault("BLUESKY_HANDLE", ""), "BlueSky handle (e.g. user.bsky.social)")
	flag.StringVar(&password, "password", envOrDefault("BLUESKY_APP_PASSWORD", ""), "BlueSky app password")
	flag.StringVar(&pds, "pds", envOrDefault("BLUESKY_PDS", "https://bsky.social"), "PDS service URL")
	flag.StringVar(&serviceDID, "service-did", envOrDefault("FEEDGEN_SERVICE_DID", ""), "Feed generator service DID (e.g. did:web:feed.example.com)")
	flag.StringVar(&feedRKey, "rkey", "", "Record key / short name for the feed (e.g. my-cool-feed)")
	flag.StringVar(&displayName, "name", "", "Feed display name (max 24 graphemes)")
	flag.StringVar(&description, "description", "", "Feed description (max 300 graphemes)")
	flag.StringVar(&avatarPath, "avatar-path", "", "Path to avatar image (PNG or JPEG)")
	flag.BoolVar(&unpublish, "unpublish", false, "Delete the feed generator record instead of publishing")
	flag.Parse()

	if handle == "" || password == "" {
		return fmt.Errorf("--handle and --password are required (or set BLUESKY_HANDLE and BLUESKY_APP_PASSWORD)")
	}
	if feedRKey == "" {
		return fmt.Errorf("--rkey is required")
	}

	ctx := context.Background()
	client := bluesky.NewClient(pds)

	fmt.Printf("Logging in as %s...\n", handle)
	if err := client.Login(ctx, handle, password); err != nil {
		return err
	}
	fmt.Printf("Authenticated as %s\n", client.DID())

	// Handle avatar upload if path provided
	var avatarRef *bluesky.BlobRef
	if avatarPath != "" {
		mimeType, err := detectMimeType(avatarPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v, skipping avatar upload\n", err)
		} else {
			imgData, err := os.ReadFile(avatarPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to read avatar file: %v, skipping avatar upload\n", err)
			} else {
				fmt.Printf("Uploading avatar from %s...\n", avatarPath)
				avatarRef, err = client.UploadBlob(ctx, imgData, mimeType)
				if err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to upload avatar: %v, continuing without avatar\n", err)
					avatarRef = nil
				} else {
					fmt.Printf("Avatar uploaded successfully (CID: %s, size: %d bytes, type: %s)\n",
						avatarRef.Ref.Link, avatarRef.Size, avatarRef.MimeType)
				}
			}
		}
	}

	if unpublish {
		fmt.Printf("Unpublishing feed %q...\n", feedRKey)
		if err := client.UnpublishFeedGenerator(ctx, feedRKey); err != nil {
			return err
		}
		fmt.Printf("Feed unpublished: at://%s/app.bsky.feed.generator/%s\n", client.DID(), feedRKey)
		return nil
	}

	if serviceDID == "" {
		return fmt.Errorf("--service-did is required for publishing (or set FEEDGEN_SERVICE_DID)")
	}
	if displayName == "" {
		return fmt.Errorf("--name is required for publishing")
	}

	record := bluesky.FeedGeneratorRecord{
		DID:         serviceDID,
		DisplayName: displayName,
		Description: description,
		Avatar:      avatarRef,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	fmt.Printf("Publishing feed %q...\n", feedRKey)
	fmt.Printf("Feed record %v\n", record)
	if err := client.PublishFeedGenerator(ctx, feedRKey, record); err != nil {
		return err
	}

	feedURI := fmt.Sprintf("at://%s/app.bsky.feed.generator/%s", client.DID(), feedRKey)
	fmt.Printf("Feed published: %s\n", feedURI)

	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// detectMimeType determines MIME type from file extension
func detectMimeType(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png", nil
	case ".jpg", ".jpeg":
		return "image/jpeg", nil
	default:
		return "", fmt.Errorf("unsupported file extension %q: expected .png, .jpg, or .jpeg", ext)
	}
}
