package transport

import "fmt"

const (
	// DefaultMaxIncomingFrameBytes — 256 KiB; WS SetReadLimit и ENet packet check.
	DefaultMaxIncomingFrameBytes = 256 * 1024
)

// TransportConfig — лимиты data plane (WS + ENet).
type TransportConfig struct {
	// MaxIncomingFrameBytes — макс. размер входящего кадра (OpCode + payload) и ENet admit-пакета; 0 — без лимита.
	MaxIncomingFrameBytes int
}

// Validate проверяет допустимость значений.
func (c TransportConfig) Validate() error {
	if c.MaxIncomingFrameBytes < 0 {
		return fmt.Errorf("transport config: max incoming frame bytes must be >= 0")
	}
	return nil
}
