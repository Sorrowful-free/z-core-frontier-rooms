package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	admissionadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"
	httpauthadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httpauth"
)

const (
	envAdmissionSecret   = "ADMISSION_SECRET"
	envAdmissionTTL      = "ADMISSION_TTL"
	envAdmissionPassword = "ADMISSION_PASSWORD"

	envHTTPAPIKey      = "HTTP_API_KEY"
	envHTTPAuthDisabled = "HTTP_AUTH_DISABLED"
)

// LoadFromEnv читает переменные окружения и собирает Config.
func LoadFromEnv() (*Config, error) {
	admission, err := loadAdmissionFromEnv()
	if err != nil {
		return nil, err
	}
	httpAuth, err := loadHTTPAuthFromEnv()
	if err != nil {
		return nil, err
	}
	return &Config{
		Admission: admission,
		HTTPAuth:  httpAuth,
	}, nil
}

func loadAdmissionFromEnv() (admissionadapter.AdmissionConfig, error) {
	ttl := admissionadapter.DefaultAdmissionTTL()
	if raw := os.Getenv(envAdmissionTTL); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return admissionadapter.AdmissionConfig{}, fmt.Errorf("%s: %w", envAdmissionTTL, err)
		}
		ttl = parsed
	}

	cfg := admissionadapter.AdmissionConfig{
		Secret:   []byte(os.Getenv(envAdmissionSecret)),
		TTL:      ttl,
		Password: os.Getenv(envAdmissionPassword),
	}
	if err := cfg.Validate(); err != nil {
		return admissionadapter.AdmissionConfig{}, err
	}
	return cfg, nil
}

func loadHTTPAuthFromEnv() (httpauthadapter.HTTPAuthConfig, error) {
	cfg := httpauthadapter.HTTPAuthConfig{
		APIKey:   os.Getenv(envHTTPAPIKey),
		Disabled: strings.EqualFold(os.Getenv(envHTTPAuthDisabled), "true"),
	}
	if err := cfg.Validate(); err != nil {
		return httpauthadapter.HTTPAuthConfig{}, err
	}
	return cfg, nil
}
