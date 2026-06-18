package zap

import (
	"fmt"

	uberzap "go.uber.org/zap"
)

const (
	LogModeProd    = "prod"
	LogModeDev     = "dev"
	DefaultLogMode = LogModeProd
)

// RootConfig — корневой zap.Logger процесса (cmd/rooms).
type RootConfig struct {
	Mode string
}

func (c RootConfig) effectiveMode() string {
	if c.Mode == "" {
		return DefaultLogMode
	}
	return c.Mode
}

func (c RootConfig) Validate() error {
	switch c.effectiveMode() {
	case LogModeProd, LogModeDev:
		return nil
	default:
		return fmt.Errorf("logging config: invalid LOG_MODE %q, want prod or dev", c.Mode)
	}
}

func (c RootConfig) NewLogger() (*uberzap.Logger, error) {
	switch c.effectiveMode() {
	case LogModeDev:
		return uberzap.NewDevelopment()
	default:
		return uberzap.NewProduction()
	}
}
