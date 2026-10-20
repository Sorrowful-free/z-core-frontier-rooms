package http_test

import (
	"net/http"
	"testing"

	httpauthadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httpauth"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
)

func TestHTTP_CreateRoom_UnauthorizedWithoutAPIKey(t *testing.T) {
	env := env.New(t, env.WithHTTPAuth(httpauthadapter.HTTPAuthConfig{APIKey: "test-http-api-key-16b"}))

	resp, _ := env.Do(t, http.MethodPost, "/rooms", createRoomRequest(nil))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHTTP_CreateRoom_AuthorizedWithBearer(t *testing.T) {
	env := env.New(t, env.WithHTTPAuth(httpauthadapter.HTTPAuthConfig{APIKey: "test-http-api-key-16b"}))

	resp, body := env.DoWithHeaders(t, http.MethodPost, "/rooms", createRoomRequest(nil), "Authorization", "Bearer test-http-api-key-16b")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestHTTP_CreateRoom_AuthorizedWithXAPIKey(t *testing.T) {
	env := env.New(t, env.WithHTTPAuth(httpauthadapter.HTTPAuthConfig{APIKey: "test-http-api-key-16b"}))

	resp, body := env.DoWithHeaders(t, http.MethodPost, "/rooms", createRoomRequest(nil), "X-API-Key", "test-http-api-key-16b")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}
