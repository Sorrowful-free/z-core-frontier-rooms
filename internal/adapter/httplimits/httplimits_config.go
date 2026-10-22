package httplimits

import "fmt"

const (
	DefaultMaxRooms              = 500
	DefaultCreateRoomsPerMinute  = 30
	DefaultIssueTicketsPerMinute = 60
	DefaultDeleteRoomsPerMinute  = 30
	DefaultListRoomsPerMinute    = 60
)

// HTTPLimitsConfig — лимиты REST control plane и ёмкость инстанса.
type HTTPLimitsConfig struct {
	// MaxRooms — максимум комнат в процессе; 0 — без лимита.
	MaxRooms int
	// CreateRoomsPerMinute — POST /rooms на IP в минуту; 0 — без лимита.
	CreateRoomsPerMinute int
	// IssueTicketsPerMinute — POST /rooms/:id/tickets на IP в минуту; 0 — без лимита.
	IssueTicketsPerMinute int
	// DeleteRoomsPerMinute — DELETE /rooms/:id на IP в минуту; 0 — без лимита.
	DeleteRoomsPerMinute int
	// ListRoomsPerMinute — GET /rooms на IP в минуту; 0 — без лимита.
	ListRoomsPerMinute int
}

// Validate проверяет, что значения не отрицательные.
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
