package config_test

import (
	"strings"
	"testing"
	"time"

	appconfig "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/config"
	httplimitsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httplimits"
	reservationadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/reservation"
	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
)

func validSecret() string {
	return "test-admission-secret-32-chars-min!!"
}

func validHTTPAPIKey() string {
	return "test-http-api-key-16b"
}

func setHTTPAuthEnabled(t *testing.T) {
	t.Helper()
	t.Setenv("HTTP_API_KEY", validHTTPAPIKey())
	t.Setenv("HTTP_AUTH_DISABLED", "")
}

func setHTTPAuthDisabled(t *testing.T) {
	t.Helper()
	t.Setenv("HTTP_API_KEY", "")
	t.Setenv("HTTP_AUTH_DISABLED", "true")
}

func TestLoadFromEnv_AdmissionRequired(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", "")
	t.Setenv("ADMISSION_TTL", "")
	t.Setenv("ADMISSION_PASSWORD", "")
	setHTTPAuthDisabled(t)

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
	if !strings.Contains(err.Error(), "secret is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFromEnv_ForbiddenDefaultSecret(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", "dev-secret-change-me")
	t.Setenv("ADMISSION_TTL", "")
	t.Setenv("ADMISSION_PASSWORD", "")
	setHTTPAuthDisabled(t)

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for forbidden secret")
	}
	if !strings.Contains(err.Error(), "forbidden default secret") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFromEnv_SecretTooShort(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", "short")
	t.Setenv("ADMISSION_TTL", "")
	t.Setenv("ADMISSION_PASSWORD", "")
	setHTTPAuthDisabled(t)

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for short secret")
	}
	if !strings.Contains(err.Error(), "at least 32 bytes") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFromEnv_InvalidTTL(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	t.Setenv("ADMISSION_TTL", "not-a-duration")
	t.Setenv("ADMISSION_PASSWORD", "")
	setHTTPAuthDisabled(t)

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for invalid ttl")
	}
	if !strings.Contains(err.Error(), "ADMISSION_TTL") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFromEnv_HTTPAPIKeyRequired(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	t.Setenv("ADMISSION_TTL", "")
	t.Setenv("ADMISSION_PASSWORD", "")
	t.Setenv("HTTP_API_KEY", "")
	t.Setenv("HTTP_AUTH_DISABLED", "")

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for missing api key")
	}
	if !strings.Contains(err.Error(), "api key is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFromEnv_HTTPAPIKeyTooShort(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	t.Setenv("HTTP_API_KEY", "short")
	t.Setenv("HTTP_AUTH_DISABLED", "")

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for short api key")
	}
	if !strings.Contains(err.Error(), "at least 16 bytes") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFromEnv_SuccessDefaults(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	t.Setenv("ADMISSION_TTL", "")
	t.Setenv("ADMISSION_PASSWORD", "")
	setHTTPAuthEnabled(t)

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if string(cfg.Admission.Secret) != validSecret() {
		t.Fatalf("secret = %q", cfg.Admission.Secret)
	}
	if cfg.Admission.TTL != time.Hour {
		t.Fatalf("ttl = %v, want 1h", cfg.Admission.TTL)
	}
	if cfg.Admission.Password != "" {
		t.Fatalf("password = %q", cfg.Admission.Password)
	}
	if cfg.HTTPAuth.APIKey != validHTTPAPIKey() {
		t.Fatalf("api key = %q", cfg.HTTPAuth.APIKey)
	}
	if cfg.HTTPAuth.Disabled {
		t.Fatal("http auth should be enabled")
	}
	if cfg.HTTPLimits.MaxRooms != httplimitsadapter.DefaultMaxRooms {
		t.Fatalf("max rooms = %d", cfg.HTTPLimits.MaxRooms)
	}
	if cfg.HTTPLimits.CreateRoomsPerMinute != httplimitsadapter.DefaultCreateRoomsPerMinute {
		t.Fatalf("create rate = %d", cfg.HTTPLimits.CreateRoomsPerMinute)
	}
	if cfg.HTTPLimits.IssueTicketsPerMinute != httplimitsadapter.DefaultIssueTicketsPerMinute {
		t.Fatalf("issue rate = %d", cfg.HTTPLimits.IssueTicketsPerMinute)
	}
	if cfg.Reservation.OrphanAdmittedTTL != reservationadapter.DefaultOrphanAdmittedTTL {
		t.Fatalf("orphan admitted ttl = %v", cfg.Reservation.OrphanAdmittedTTL)
	}
	if cfg.Reservation.OrphanSweepInterval != reservationadapter.DefaultOrphanSweepInterval {
		t.Fatalf("orphan sweep interval = %v", cfg.Reservation.OrphanSweepInterval)
	}
	if cfg.Transport.MaxIncomingFrameBytes != adaptertransport.DefaultMaxIncomingFrameBytes {
		t.Fatalf("max incoming frame bytes = %d", cfg.Transport.MaxIncomingFrameBytes)
	}
}

func TestLoadFromEnv_SuccessCustomTTLAndPassword(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	t.Setenv("ADMISSION_TTL", "30m")
	t.Setenv("ADMISSION_PASSWORD", "room-gate")
	setHTTPAuthEnabled(t)

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if cfg.Admission.TTL != 30*time.Minute {
		t.Fatalf("ttl = %v", cfg.Admission.TTL)
	}
	if cfg.Admission.Password != "room-gate" {
		t.Fatalf("password = %q", cfg.Admission.Password)
	}
}

func TestLoadFromEnv_HTTPAuthDisabled(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	setHTTPAuthDisabled(t)

	cfg, err := appconfig.LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if !cfg.HTTPAuth.Disabled {
		t.Fatal("expected http auth disabled")
	}
}
