package reservation

import (
	"fmt"
	"time"
)

const (
	DefaultOrphanAdmittedTTL    = 30 * time.Second
	DefaultOrphanSweepInterval  = 10 * time.Second
)

// ReservationConfig — фоновый orphan sweep admitted-слотов без peer в room.
type ReservationConfig struct {
	// OrphanAdmittedTTL — grace после Admit до revoke orphan без peer; 0 — сразу при sweep.
	OrphanAdmittedTTL time.Duration
	// OrphanSweepInterval — период sweep; 0 — отключить фоновый sweep.
	OrphanSweepInterval time.Duration
}

// Validate проверяет, что длительности не отрицательные.
func (c ReservationConfig) Validate() error {
	if c.OrphanAdmittedTTL < 0 {
		return fmt.Errorf("reservation config: orphan admitted TTL must be >= 0")
	}
	if c.OrphanSweepInterval < 0 {
		return fmt.Errorf("reservation config: orphan sweep interval must be >= 0")
	}
	return nil
}
