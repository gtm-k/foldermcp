package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
)

// APIKeyAuth validates requests using a Bearer token in the Authorization header.
type APIKeyAuth struct {
	Key string
}

// NewAPIKeyAuth returns an APIKeyAuth that checks for the given key.
func NewAPIKeyAuth(key string) *APIKeyAuth {
	return &APIKeyAuth{Key: key}
}

// Middleware returns an http.Handler that rejects requests without a valid
// Authorization: Bearer <key> header. Requests with a matching key are
// passed through to next; all others receive 401 Unauthorized.
func (a *APIKeyAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}

		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
			return
		}

		token := auth[len(prefix):]
		// Constant-time comparison to prevent timing attacks.
		if subtle.ConstantTimeCompare([]byte(token), []byte(a.Key)) != 1 {
			http.Error(w, "invalid api key", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// GenerateAPIKey produces a random API key with the prefix "fmcp_k_" followed
// by 32 lowercase hex characters (16 random bytes), for a total of 39 characters.
func GenerateAPIKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return "fmcp_k_" + hex.EncodeToString(b)
}
