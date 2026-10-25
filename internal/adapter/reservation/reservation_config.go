package reservation

import (
	"fmt"
	"time"
)

const (
	DefaultReconnectTTL        = 30 * time.Second
	DefaultOrphanSweepInterval = 10 * time.Second
)

// ReservationConfig — reconnect grace admitted-слотов без peer в room (P20).
type ReservationConfig struct {
	// ReconnectTTL — grace от lastSeen (admittedAt слота) до revoke orphan;
	// 0 — сразу при sweep. Заменяет OrphanAdmittedTTL: при едином timestamp
	// lastSeen роли TTL слота и grace сливаются.
	ReconnectTTL time.Duration
	// OrphanSweepInterval — период sweep; 0 — отключить фоновый sweep.
	OrphanSweepInterval time.Duration
}

// Validate проверяет, что длительности не отрицательные.
func (c ReservationConfig) Validate() error {
	if c.ReconnectTTL < 0 {
		return fmt.Errorf("reservation config: reconnect TTL must be >= 0")
	}
	if c.OrphanSweepInterval < 0 {
		return fmt.Errorf("reservation config: orphan sweep interval must be >= 0")
	}
	return nil
}
