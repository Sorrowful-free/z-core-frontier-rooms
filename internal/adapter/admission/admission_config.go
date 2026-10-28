package admission

import (
	"errors"
	"fmt"
	"time"
)

const (
	minAdmissionSecretLen    = 32
	forbiddenAdmissionSecret = "dev-secret-change-me"
	defaultAdmissionTTL      = time.Hour
)

// DefaultAdmissionTTL — default TTL used when ADMISSION_TTL is not set.
func DefaultAdmissionTTL() time.Duration {
	return defaultAdmissionTTL
}

// AdmissionConfig — parameters for constructing Admission (secret, TTL).
type AdmissionConfig struct {
	Secret []byte
	TTL    time.Duration
}

// Validate checks the configuration before use in NewAdmission.
func (c AdmissionConfig) Validate() error {
	if len(c.Secret) == 0 {
		return errors.New("admission config: secret is required")
	}
	if string(c.Secret) == forbiddenAdmissionSecret {
		return errors.New("admission config: forbidden default secret")
	}
	if len(c.Secret) < minAdmissionSecretLen {
		return fmt.Errorf("admission config: secret must be at least %d bytes", minAdmissionSecretLen)
	}
	if c.TTL <= 0 {
		return errors.New("admission config: ttl must be positive")
	}
	return nil
}
