package reservation

import (
	"fmt"
	"time"
)

const (
	DefaultReconnectTTL        = 30 * time.Second
	DefaultOrphanSweepInterval = 10 * time.Second
)

// ReservationConfig — reconnect grace for admitted slots with no peer in the room (P20).
type ReservationConfig struct {
	// ReconnectTTL — grace from the slot's lastSeen until orphan revoke;
	// 0 — immediate at sweep. The single lastSeen timestamp serves both
	// the slot TTL and post-disconnect grace.
	ReconnectTTL time.Duration
	// OrphanSweepInterval — sweep period; 0 — disable the background sweep.
	OrphanSweepInterval time.Duration
}

// Validate checks that durations are non-negative.
func (c ReservationConfig) Validate() error {
	if c.ReconnectTTL < 0 {
		return fmt.Errorf("reservation config: reconnect TTL must be >= 0")
	}
	if c.OrphanSweepInterval < 0 {
		return fmt.Errorf("reservation config: orphan sweep interval must be >= 0")
	}
	return nil
}
