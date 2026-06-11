package httplimits_test

import (
	"errors"
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
	if !limits.AllowHTTPCreate(ip) || !limits.AllowHTTPCreate(ip) {
		t.Fatal("first two create requests should pass")
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
