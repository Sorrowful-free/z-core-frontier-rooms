package metrics

import (
	"fmt"
	"net"
)

// MetricsConfig — process telemetry (P19).
type MetricsConfig struct {
	// AdminListenAddr — address of the separate admin Fiber listener with
	// `GET /metrics` (env `ADMIN_LISTEN_ADDR`). Empty — telemetry
	// endpoint disabled (signal collection still runs, no scrape).
	AdminListenAddr string
}

// Validate: an empty address is legal (endpoint disabled); a set address
// must resolve as a TCP listen addr.
func (c MetricsConfig) Validate() error {
	if c.AdminListenAddr == "" {
		return nil
	}
	if _, err := net.ResolveTCPAddr("tcp", c.AdminListenAddr); err != nil {
		return fmt.Errorf("metrics config: admin listen addr %q: %w", c.AdminListenAddr, err)
	}
	return nil
}
