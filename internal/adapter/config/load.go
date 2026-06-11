package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	admissionadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"
	httpauthadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httpauth"
	httplimitsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httplimits"
)

const (
	envAdmissionSecret   = "ADMISSION_SECRET"
	envAdmissionTTL      = "ADMISSION_TTL"
	envAdmissionPassword = "ADMISSION_PASSWORD"

	envHTTPAPIKey       = "HTTP_API_KEY"
	envHTTPAuthDisabled = "HTTP_AUTH_DISABLED"

	envMaxRooms              = "MAX_ROOMS"
	envHTTPCreateRatePerMin  = "HTTP_CREATE_RATE_PER_MIN"
	envHTTPIssueRatePerMin   = "HTTP_ISSUE_RATE_PER_MIN"
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
	httpLimits, err := loadHTTPLimitsFromEnv()
	if err != nil {
		return nil, err
	}
	return &Config{
		Admission:  admission,
		HTTPAuth:   httpAuth,
		HTTPLimits: httpLimits,
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

func loadHTTPLimitsFromEnv() (httplimitsadapter.HTTPLimitsConfig, error) {
	maxRooms, err := intFromEnv(envMaxRooms, httplimitsadapter.DefaultMaxRooms)
	if err != nil {
		return httplimitsadapter.HTTPLimitsConfig{}, err
	}
	createRate, err := intFromEnv(envHTTPCreateRatePerMin, httplimitsadapter.DefaultCreateRoomsPerMinute)
	if err != nil {
		return httplimitsadapter.HTTPLimitsConfig{}, err
	}
	issueRate, err := intFromEnv(envHTTPIssueRatePerMin, httplimitsadapter.DefaultIssueTicketsPerMinute)
	if err != nil {
		return httplimitsadapter.HTTPLimitsConfig{}, err
	}

	cfg := httplimitsadapter.HTTPLimitsConfig{
		MaxRooms:              maxRooms,
		CreateRoomsPerMinute:  createRate,
		IssueTicketsPerMinute: issueRate,
	}
	if err := cfg.Validate(); err != nil {
		return httplimitsadapter.HTTPLimitsConfig{}, err
	}
	return cfg, nil
}

func intFromEnv(name string, defaultValue int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return value, nil
}
