package config

import (
	"fmt"
	"os"
	"time"

	admissionadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"
)

const (
	envAdmissionSecret   = "ADMISSION_SECRET"
	envAdmissionTTL      = "ADMISSION_TTL"
	envAdmissionPassword = "ADMISSION_PASSWORD"
)

// LoadFromEnv читает переменные окружения и собирает Config.
func LoadFromEnv() (*Config, error) {
	admission, err := loadAdmissionFromEnv()
	if err != nil {
		return nil, err
	}
	return &Config{Admission: admission}, nil
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
