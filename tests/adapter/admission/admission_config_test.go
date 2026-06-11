package admission_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/admission"
)

func TestAdmissionConfig_Validate(t *testing.T) {
	t.Parallel()

	valid := admission.AdmissionConfig{
		Secret: []byte("test-admission-secret-32-chars-min!!"),
		TTL:    time.Hour,
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}

	tests := []struct {
		name    string
		cfg     admission.AdmissionConfig
		contain string
	}{
		{
			name:    "empty secret",
			cfg:     admission.AdmissionConfig{TTL: time.Hour},
			contain: "secret is required",
		},
		{
			name:    "short secret",
			cfg:     admission.AdmissionConfig{Secret: []byte("short"), TTL: time.Hour},
			contain: "at least 32 bytes",
		},
		{
			name: "forbidden default",
			cfg: admission.AdmissionConfig{
				Secret: []byte("dev-secret-change-me"),
				TTL:    time.Hour,
			},
			contain: "forbidden default secret",
		},
		{
			name: "non positive ttl",
			cfg: admission.AdmissionConfig{
				Secret: []byte("test-admission-secret-32-chars-min!!"),
				TTL:    0,
			},
			contain: "ttl must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.Validate()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.contain) {
				t.Fatalf("err = %v, want substring %q", err, tt.contain)
			}
		})
	}
}
