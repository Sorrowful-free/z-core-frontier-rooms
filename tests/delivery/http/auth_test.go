package http_test

import (
	"net/http"
	"testing"

	httpauthadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httpauth"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
)

func TestHTTP_CreateRoom_UnauthorizedWithoutAPIKey(t *testing.T) {
	e := env.New(t, env.WithHTTPAuth(httpauthadapter.HTTPAuthConfig{APIKey: "test-http-api-key-16b"}))

	resp, _ := e.Do(t, http.MethodPost, "/rooms", createRoomRequest(nil))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHTTP_CreateRoom_AuthorizedWithBearer(t *testing.T) {
	e := env.New(t, env.WithHTTPAuth(httpauthadapter.HTTPAuthConfig{APIKey: "test-http-api-key-16b"}))

	resp, body := e.DoWithHeaders(t, http.MethodPost, "/rooms", createRoomRequest(nil), "Authorization", "Bearer test-http-api-key-16b")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestHTTP_CreateRoom_AuthorizedWithXAPIKey(t *testing.T) {
	e := env.New(t, env.WithHTTPAuth(httpauthadapter.HTTPAuthConfig{APIKey: "test-http-api-key-16b"}))

	resp, body := e.DoWithHeaders(t, http.MethodPost, "/rooms", createRoomRequest(nil), "X-API-Key", "test-http-api-key-16b")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}
