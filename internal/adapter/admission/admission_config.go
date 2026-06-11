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

// DefaultAdmissionTTL — TTL по умолчанию, если ADMISSION_TTL не задан.
func DefaultAdmissionTTL() time.Duration {
	return defaultAdmissionTTL
}

// AdmissionConfig — параметры конструирования Admission (секрет, TTL, опциональный глобальный пароль Issue).
type AdmissionConfig struct {
	Secret   []byte
	TTL      time.Duration
	Password string
}

// Validate проверяет конфигурацию перед использованием в NewAdmission.
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
