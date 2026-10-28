package httpauth

import (
	"errors"
	"fmt"
)

const minHTTPAPIKeyLen = 16

// HTTPAuthConfig — REST control plane authentication (API key).
type HTTPAuthConfig struct {
	// APIKey — expected key; used when Disabled == false.
	APIKey string
	// Disabled — disable the check (local dev only).
	Disabled bool
}

// Validate checks the configuration before startup.
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
