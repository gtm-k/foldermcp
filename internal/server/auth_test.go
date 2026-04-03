package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIKeyAuth_ValidKey(t *testing.T) {
	auth := NewAPIKeyAuth("fmcp_k_abc123")

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := auth.Middleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer fmcp_k_abc123")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !called {
		t.Error("inner handler was not called")
	}
}

func TestAPIKeyAuth_InvalidKey(t *testing.T) {
	auth := NewAPIKeyAuth("fmcp_k_correct")

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called for invalid key")
	})

	handler := auth.Middleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer fmcp_k_wrong")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestAPIKeyAuth_MissingHeader(t *testing.T) {
	auth := NewAPIKeyAuth("fmcp_k_somekey")

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called when header is missing")
	})

	handler := auth.Middleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No Authorization header set.
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestGenerateAPIKey(t *testing.T) {
	key := GenerateAPIKey()

	if len(key) != 39 {
		t.Errorf("key length = %d, want 39; key = %q", len(key), key)
	}

	prefix := "fmcp_k_"
	if key[:7] != prefix {
		t.Errorf("key prefix = %q, want %q", key[:7], prefix)
	}

	// Hex chars only after prefix.
	for i, c := range key[7:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("key[%d] = %c, want hex char", i+7, c)
		}
	}

	// Ensure uniqueness (probabilistic but effectively deterministic).
	key2 := GenerateAPIKey()
	if key == key2 {
		t.Error("GenerateAPIKey returned the same key twice")
	}
}
