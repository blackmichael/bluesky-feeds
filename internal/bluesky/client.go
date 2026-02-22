package bluesky

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultPDS = "https://bsky.social"

// Client is a minimal BlueSky/AT Protocol API client for managing feed
// generator records.
type Client struct {
	pds        string
	httpClient *http.Client

	// populated after Login
	accessJwt string
	did       string
}

// NewClient creates a new BlueSky API client. If pds is empty, it defaults to
// https://bsky.social.
func NewClient(pds string) *Client {
	if pds == "" {
		pds = defaultPDS
	}
	return &Client{
		pds: pds,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Login authenticates with the PDS and stores the session token. Use an App
// Password, not your account password.
func (c *Client) Login(ctx context.Context, identifier, password string) error {
	body := map[string]string{
		"identifier": identifier,
		"password":   password,
	}

	var resp createSessionResponse
	if err := c.post(ctx, "/xrpc/com.atproto.server.createSession", body, &resp); err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	c.accessJwt = resp.AccessJwt
	c.did = resp.DID
	return nil
}

// DID returns the authenticated user's DID. Only valid after Login.
func (c *Client) DID() string {
	return c.did
}

// BlobRef represents an AT Protocol blob reference for uploaded content.
type BlobRef struct {
	Type string `json:"$type"`
	Ref  struct {
		Link string `json:"$link"`
	} `json:"ref"`
	MimeType string `json:"mimeType"`
	Size     int    `json:"size"`
}

// FeedGeneratorRecord is the record body for app.bsky.feed.generator.
type FeedGeneratorRecord struct {
	DID         string   `json:"did"`
	DisplayName string   `json:"displayName"`
	Description string   `json:"description,omitempty"`
	Avatar      *BlobRef `json:"avatar,omitempty"`
	CreatedAt   string   `json:"createdAt"`
}

// PublishFeedGenerator creates or updates a feed generator record in the
// authenticated user's repo via com.atproto.repo.putRecord.
func (c *Client) PublishFeedGenerator(ctx context.Context, rkey string, record FeedGeneratorRecord) error {
	if c.accessJwt == "" {
		return fmt.Errorf("not authenticated: call Login first")
	}

	body := putRecordRequest{
		Repo:       c.did,
		Collection: "app.bsky.feed.generator",
		RKey:       rkey,
		Record:     record,
	}

	var resp json.RawMessage
	if err := c.post(ctx, "/xrpc/com.atproto.repo.putRecord", body, &resp); err != nil {
		return fmt.Errorf("put record: %w", err)
	}

	return nil
}

// UnpublishFeedGenerator deletes a feed generator record from the
// authenticated user's repo via com.atproto.repo.deleteRecord.
func (c *Client) UnpublishFeedGenerator(ctx context.Context, rkey string) error {
	if c.accessJwt == "" {
		return fmt.Errorf("not authenticated: call Login first")
	}

	body := deleteRecordRequest{
		Repo:       c.did,
		Collection: "app.bsky.feed.generator",
		RKey:       rkey,
	}

	var resp json.RawMessage
	if err := c.post(ctx, "/xrpc/com.atproto.repo.deleteRecord", body, &resp); err != nil {
		return fmt.Errorf("delete record: %w", err)
	}

	return nil
}

// UploadBlob uploads raw image bytes as a blob and returns a reference.
// The blob will be deleted if not referenced in a record within a time window.
func (c *Client) UploadBlob(ctx context.Context, data []byte, mimeType string) (*BlobRef, error) {
	if c.accessJwt == "" {
		return nil, fmt.Errorf("not authenticated: call Login first")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.pds+"/xrpc/com.atproto.repo.uploadBlob", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", mimeType)
	req.Header.Set("Authorization", "Bearer "+c.accessJwt)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result uploadBlobResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &result.Blob, nil
}

func (c *Client) post(ctx context.Context, path string, body any, result any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.pds+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.accessJwt != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessJwt)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}

	return nil
}

type createSessionResponse struct {
	AccessJwt string `json:"accessJwt"`
	DID       string `json:"did"`
	Handle    string `json:"handle"`
}

type putRecordRequest struct {
	Repo       string `json:"repo"`
	Collection string `json:"collection"`
	RKey       string `json:"rkey"`
	Record     any    `json:"record"`
}

type deleteRecordRequest struct {
	Repo       string `json:"repo"`
	Collection string `json:"collection"`
	RKey       string `json:"rkey"`
}

type uploadBlobResponse struct {
	Blob BlobRef `json:"blob"`
}
