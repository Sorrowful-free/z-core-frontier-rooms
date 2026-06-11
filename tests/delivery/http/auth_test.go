package http_test

import (
	"net/http"
	"testing"

	httpauthadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httpauth"
)

func TestHTTP_CreateRoom_UnauthorizedWithoutAPIKey(t *testing.T) {
	env := newTestEnv(t, httpauthadapter.HTTPAuthConfig{APIKey: "test-http-api-key-16b"})

	resp, _ := env.do(t, http.MethodPost, "/rooms", map[string]any{"capacity": 4})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHTTP_CreateRoom_AuthorizedWithBearer(t *testing.T) {
	env := newTestEnv(t, httpauthadapter.HTTPAuthConfig{APIKey: "test-http-api-key-16b"})

	resp, body := env.doAuthorized(t, http.MethodPost, "/rooms", map[string]any{"capacity": 4}, "Bearer test-http-api-key-16b")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestHTTP_CreateRoom_AuthorizedWithXAPIKey(t *testing.T) {
	env := newTestEnv(t, httpauthadapter.HTTPAuthConfig{APIKey: "test-http-api-key-16b"})

	resp, body := env.doWithHeader(t, http.MethodPost, "/rooms", map[string]any{"capacity": 4}, "X-API-Key", "test-http-api-key-16b")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}
