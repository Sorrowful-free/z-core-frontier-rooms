package metrics

import (
	"fmt"
	"net"
)

// MetricsConfig — телеметрия процесса (P19).
type MetricsConfig struct {
	// AdminListenAddr — адрес отдельного admin Fiber listener'а с
	// `GET /metrics` (env `ADMIN_LISTEN_ADDR`). Пустой — telemetry
	// endpoint выключен (коллекция сигналов работает, scrape нет).
	AdminListenAddr string
}

// Validate: пустой адрес легален (endpoint отключён); заданный должен
// разрешаться как TCP listen addr.
func (c MetricsConfig) Validate() error {
	if c.AdminListenAddr == "" {
		return nil
	}
	if _, err := net.ResolveTCPAddr("tcp", c.AdminListenAddr); err != nil {
		return fmt.Errorf("metrics config: admin listen addr %q: %w", c.AdminListenAddr, err)
	}
	return nil
}
