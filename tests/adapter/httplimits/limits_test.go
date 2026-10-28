package httplimits_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httplimits"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	porthttplimits "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/httplimits"
)

func mustNewLimits(t *testing.T, cfg httplimits.HTTPLimitsConfig) porthttplimits.Limits {
	t.Helper()
	limits, err := httplimits.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return limits
}

func TestLimits_AllowCreateRoom(t *testing.T) {
	t.Parallel()

	limits := mustNewLimits(t, httplimits.HTTPLimitsConfig{MaxRooms: 2})

	if err := limits.AllowCreateRoom(1); err != nil {
		t.Fatalf("under limit: %v", err)
	}
	if err := limits.AllowCreateRoom(2); !errors.Is(err, domain.ErrRoomsLimitReached) {
		t.Fatalf("at limit: %v", err)
	}
}

func TestLimits_AllowCreateRoom_Unlimited(t *testing.T) {
	t.Parallel()

	limits := mustNewLimits(t, httplimits.HTTPLimitsConfig{MaxRooms: 0})
	if err := limits.AllowCreateRoom(1000); err != nil {
		t.Fatalf("unlimited: %v", err)
	}
}

func TestLimits_HTTPRateLimit(t *testing.T) {
	t.Parallel()

	limits := mustNewLimits(t, httplimits.HTTPLimitsConfig{
		CreateRoomsPerMinute:  2,
		IssueTicketsPerMinute: 1,
	})

	const ip = "203.0.113.1"
	if !limits.AllowHTTPCreate(ip) {
		t.Fatal("first create request should pass")
	}
	if !limits.AllowHTTPCreate(ip) {
		t.Fatal("second create request should pass")
	}
	if limits.AllowHTTPCreate(ip) {
		t.Fatal("third create request should be denied")
	}

	if !limits.AllowHTTPIssueTicket(ip) {
		t.Fatal("first issue request should pass")
	}
	if limits.AllowHTTPIssueTicket(ip) {
		t.Fatal("second issue request should be denied")
	}
}

func TestLimits_HTTPRateLimit_DeleteAndList(t *testing.T) {
	t.Parallel()

	limits := mustNewLimits(t, httplimits.HTTPLimitsConfig{
		DeleteRoomsPerMinute: 1,
		ListRoomsPerMinute:   1,
	})

	const ip = "203.0.113.2"
	if !limits.AllowHTTPDelete(ip) {
		t.Fatal("first delete request should pass")
	}
	if limits.AllowHTTPDelete(ip) {
		t.Fatal("second delete request should be denied")
	}
	if !limits.AllowHTTPList(ip) {
		t.Fatal("first list request should pass")
	}
	if limits.AllowHTTPList(ip) {
		t.Fatal("second list request should be denied")
	}
}

// TestLimits_HTTPRateLimit_KeyRotation: many unique IPs (rotation) —
// each gets its own window, the per-IP limit keeps working.
func TestLimits_HTTPRateLimit_KeyRotation(t *testing.T) {
	t.Parallel()

	limits := mustNewLimits(t, httplimits.HTTPLimitsConfig{CreateRoomsPerMinute: 1})

	const n = 512
	for i := 0; i < n; i++ {
		ip := fmt.Sprintf("198.51.100.%d", i)
		if !limits.AllowHTTPCreate(ip) {
			t.Fatalf("first request for new key %d should pass", i)
		}
		if limits.AllowHTTPCreate(ip) {
			t.Fatalf("second request for key %d should be denied", i)
		}
	}
}
