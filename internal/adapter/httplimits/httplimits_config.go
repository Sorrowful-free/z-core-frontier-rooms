package httplimits

import "fmt"

const (
	DefaultMaxRooms              = 500
	DefaultCreateRoomsPerMinute  = 30
	DefaultIssueTicketsPerMinute = 60
	DefaultDeleteRoomsPerMinute  = 30
	DefaultListRoomsPerMinute    = 60
)

// HTTPLimitsConfig — REST control plane limits and instance capacity.
type HTTPLimitsConfig struct {
	// MaxRooms — maximum rooms per process; 0 — no limit.
	MaxRooms int
	// CreateRoomsPerMinute — POST /rooms per IP per minute; 0 — no limit.
	CreateRoomsPerMinute int
	// IssueTicketsPerMinute — POST /rooms/:id/tickets per IP per minute; 0 — no limit.
	IssueTicketsPerMinute int
	// DeleteRoomsPerMinute — DELETE /rooms/:id per IP per minute; 0 — no limit.
	DeleteRoomsPerMinute int
	// ListRoomsPerMinute — GET /rooms per IP per minute; 0 — no limit.
	ListRoomsPerMinute int
}

// Validate checks that values are non-negative.
func (c HTTPLimitsConfig) Validate() error {
	if c.MaxRooms < 0 {
		return fmt.Errorf("http limits config: max rooms must be >= 0")
	}
	if c.CreateRoomsPerMinute < 0 {
		return fmt.Errorf("http limits config: create rate must be >= 0")
	}
	if c.IssueTicketsPerMinute < 0 {
		return fmt.Errorf("http limits config: issue rate must be >= 0")
	}
	if c.DeleteRoomsPerMinute < 0 {
		return fmt.Errorf("http limits config: delete rate must be >= 0")
	}
	if c.ListRoomsPerMinute < 0 {
		return fmt.Errorf("http limits config: list rate must be >= 0")
	}
	return nil
}
