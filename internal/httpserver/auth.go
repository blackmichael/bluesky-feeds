package httpserver

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var (
	// errNoAuthHeader indicates the request carried no Authorization header.
	// This is normal for anonymous requests to public feeds.
	errNoAuthHeader = errors.New("no authorization header")

	// errMalformedAuth indicates the Authorization header is present but is
	// not a well-formed bearer token.
	errMalformedAuth = errors.New("malformed authorization header")

	// errInvalidToken indicates the bearer token could not be decoded or has
	// expired.
	errInvalidToken = errors.New("invalid bearer token")
)

// tokenClaims is the subset of access token (JWT) claims we care about.
type tokenClaims struct {
	// Iss is the DID that signed and issued the token (the user's PDS/account).
	Iss string `json:"iss"`

	// Sub is the subject the token was issued for; normally the same DID.
	Sub string `json:"sub"`

	// Exp is the expiry time in seconds since the Unix epoch. May be absent.
	Exp int64 `json:"exp"`
}

// requesterDID returns the DID of the user that made the request, taken from
// the Authorization bearer token. Clients (and the AppView) send the viewer's
// access token here; its "iss" claim is the viewer's DID.
//
// The token signature is NOT verified. Doing so would require resolving the
// issuer's DID document and checking an ES256K/ES256 signature, and callers
// use the returned DID only for personalization, never for authorization.
// Treat the result as an untrusted hint.
//
// If the request has no Authorization header, errNoAuthHeader is returned so
// callers can serve an anonymous view.
func requesterDID(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", errNoAuthHeader
	}

	scheme, token, ok := strings.Cut(auth, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", errMalformedAuth
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errMalformedAuth
	}

	claims, err := parseTokenClaims(token)
	if err != nil {
		return "", err
	}

	if claims.Exp != 0 && time.Now().After(time.Unix(claims.Exp, 0)) {
		return "", fmt.Errorf("%w: expired", errInvalidToken)
	}

	did := claims.Iss
	if did == "" {
		did = claims.Sub
	}
	if did == "" {
		return "", fmt.Errorf("%w: no iss or sub claim", errInvalidToken)
	}

	return did, nil
}

// parseTokenClaims decodes the payload segment of a JWT without verifying its
// signature.
func parseTokenClaims(token string) (*tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: expected 3 segments, got %d", errInvalidToken, len(parts))
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: decode payload: %w", errInvalidToken, err)
	}

	var claims tokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("%w: unmarshal payload: %w", errInvalidToken, err)
	}

	return &claims, nil
}
