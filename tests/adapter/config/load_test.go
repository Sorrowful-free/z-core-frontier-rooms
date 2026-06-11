package config_test

import (
	"strings"
	"testing"
	"time"

	appconfig "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/config"
)

func validSecret() string {
	return "test-admission-secret-32-chars-min!!"
}

func TestLoadFromEnv_AdmissionRequired(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", "")
	t.Setenv("ADMISSION_TTL", "")
	t.Setenv("ADMISSION_PASSWORD", "")

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

	_, err := appconfig.LoadFromEnv()
	if err == nil {
		t.Fatal("expected error for invalid ttl")
	}
	if !strings.Contains(err.Error(), "ADMISSION_TTL") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadFromEnv_SuccessDefaults(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	t.Setenv("ADMISSION_TTL", "")
	t.Setenv("ADMISSION_PASSWORD", "")

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
}

func TestLoadFromEnv_SuccessCustomTTLAndPassword(t *testing.T) {
	t.Setenv("ADMISSION_SECRET", validSecret())
	t.Setenv("ADMISSION_TTL", "30m")
	t.Setenv("ADMISSION_PASSWORD", "room-gate")

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
