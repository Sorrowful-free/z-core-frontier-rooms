package http_test

import (
	"net/http"
	"testing"

	httplimitsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httplimits"
)

func TestHTTP_CreateRoom_RateLimited(t *testing.T) {
	env := newTestEnvWithLimits(t, httplimitsadapter.HTTPLimitsConfig{
		CreateRoomsPerMinute: 1,
	})

	resp1, _ := env.do(t, http.MethodPost, "/rooms", map[string]any{"capacity": 4})
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("first create status = %d, want 201", resp1.StatusCode)
	}

	resp2, body := env.do(t, http.MethodPost, "/rooms", map[string]any{"capacity": 4})
	if resp2.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second create status = %d, want 429, body = %s", resp2.StatusCode, body)
	}
}

func TestHTTP_IssueTicket_RateLimited(t *testing.T) {
	env := newTestEnvWithLimits(t, httplimitsadapter.HTTPLimitsConfig{
		IssueTicketsPerMinute: 1,
	})

	createResp, _ := env.do(t, http.MethodPost, "/rooms", map[string]any{"capacity": 4})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", createResp.StatusCode)
	}

	const roomID = "1"
	issue1, _ := env.do(t, http.MethodPost, "/rooms/"+roomID+"/tickets", map[string]string{"nick_name": "player1"})
	if issue1.StatusCode != http.StatusCreated {
		t.Fatalf("first issue status = %d, want 201", issue1.StatusCode)
	}

	issue2, body := env.do(t, http.MethodPost, "/rooms/"+roomID+"/tickets", map[string]string{"nick_name": "player2"})
	if issue2.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second issue status = %d, want 429, body = %s", issue2.StatusCode, body)
	}
}

func TestHTTP_CreateRoom_RoomsLimitReached(t *testing.T) {
	env := newTestEnvWithLimits(t, httplimitsadapter.HTTPLimitsConfig{
		MaxRooms:             1,
		CreateRoomsPerMinute: 0,
	})

	resp1, _ := env.do(t, http.MethodPost, "/rooms", map[string]any{"capacity": 4})
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("first create status = %d, want 201", resp1.StatusCode)
	}

	resp2, body := env.do(t, http.MethodPost, "/rooms", map[string]any{"capacity": 4})
	if resp2.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("second create status = %d, want 503, body = %s", resp2.StatusCode, body)
	}
}
