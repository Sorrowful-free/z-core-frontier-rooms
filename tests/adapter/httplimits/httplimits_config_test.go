package httplimits_test

import (
	"strings"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httplimits"
)

func TestHTTPLimitsConfig_Validate(t *testing.T) {
	t.Parallel()

	valid := httplimits.HTTPLimitsConfig{
		MaxRooms:              100,
		CreateRoomsPerMinute:  10,
		IssueTicketsPerMinute: 20,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}

	invalid := httplimits.HTTPLimitsConfig{MaxRooms: -1}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected error for negative max rooms")
	} else if !strings.Contains(err.Error(), "max rooms") {
		t.Fatalf("err = %v", err)
	}
}
