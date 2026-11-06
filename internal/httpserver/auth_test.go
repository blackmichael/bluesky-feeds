package httpserver

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func makeToken(t *testing.T, claims string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256K","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(claims))
	sig := base64.RawURLEncoding.EncodeToString([]byte("signature"))
	return header + "." + payload + "." + sig
}

func requestWithAuth(auth string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "/xrpc/app.bsky.feed.getFeedSkeleton", nil)
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	return r
}

func TestRequesterDID(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()

	tests := []struct {
		name    string
		auth    string
		wantDID string
		wantErr error
	}{
		{
			name:    "iss claim",
			auth:    "Bearer " + makeToken(t, `{"iss":"did:plc:abc123"}`),
			wantDID: "did:plc:abc123",
		},
		{
			name:    "falls back to sub claim",
			auth:    "Bearer " + makeToken(t, `{"sub":"did:plc:sub456"}`),
			wantDID: "did:plc:sub456",
		},
		{
			name:    "ignores unrelated claims",
			auth:    "Bearer " + makeToken(t, `{"scope":"com.atproto.access","iss":"did:web:example.com","exp":`+strconv.FormatInt(future, 10)+`}`),
			wantDID: "did:web:example.com",
		},
		{
			name:    "no header",
			auth:    "",
			wantErr: errNoAuthHeader,
		},
		{
			name:    "wrong scheme",
			auth:    "Basic " + makeToken(t, `{"iss":"did:plc:abc123"}`),
			wantErr: errMalformedAuth,
		},
		{
			name:    "not a jwt",
			auth:    "Bearer not-a-jwt",
			wantErr: errInvalidToken,
		},
		{
			name:    "missing did claims",
			auth:    "Bearer " + makeToken(t, `{"scope":"com.atproto.access"}`),
			wantErr: errInvalidToken,
		},
		{
			name:    "expired",
			auth:    "Bearer " + makeToken(t, `{"iss":"did:plc:abc123","exp":1}`),
			wantErr: errInvalidToken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			did, err := requesterDID(requestWithAuth(tt.auth))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("got error %v, want %v", err, tt.wantErr)
				}
				if did != "" {
					t.Fatalf("got DID %q, want empty", did)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if did != tt.wantDID {
				t.Fatalf("got DID %q, want %q", did, tt.wantDID)
			}
		})
	}
}
