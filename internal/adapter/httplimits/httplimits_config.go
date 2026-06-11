package httplimits

import "fmt"

const (
	DefaultMaxRooms              = 500
	DefaultCreateRoomsPerMinute  = 30
	DefaultIssueTicketsPerMinute = 60
)

// HTTPLimitsConfig — лимиты REST control plane и ёмкость инстанса.
type HTTPLimitsConfig struct {
	// MaxRooms — максимум комнат в процессе; 0 — без лимита.
	MaxRooms int
	// CreateRoomsPerMinute — POST /rooms на IP в минуту; 0 — без лимита.
	CreateRoomsPerMinute int
	// IssueTicketsPerMinute — POST /rooms/:id/tickets на IP в минуту; 0 — без лимита.
	IssueTicketsPerMinute int
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
	return nil
}
