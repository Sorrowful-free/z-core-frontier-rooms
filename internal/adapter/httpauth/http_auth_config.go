package httpauth

import (
	"errors"
	"fmt"
)

const minHTTPAPIKeyLen = 16

// HTTPAuthConfig — аутентификация REST control plane (API key).
type HTTPAuthConfig struct {
	// APIKey — ожидаемый ключ; используется, если Disabled == false.
	APIKey string
	// Disabled — отключить проверку (только local dev).
	Disabled bool
}

// Validate проверяет конфигурацию перед стартом.
func (c HTTPAuthConfig) Validate() error {
	if c.Disabled {
		return nil
	}
	if c.APIKey == "" {
		return errors.New("http auth config: api key is required when auth is enabled")
	}
	if len(c.APIKey) < minHTTPAPIKeyLen {
		return fmt.Errorf("http auth config: api key must be at least %d bytes", minHTTPAPIKeyLen)
	}
	return nil
}
