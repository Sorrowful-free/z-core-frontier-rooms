package httpauth_test

import (
	"strings"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/httpauth"
)

func TestHTTPAuthConfig_Validate(t *testing.T) {
	t.Parallel()

	valid := httpauth.HTTPAuthConfig{APIKey: "valid-api-key-16b"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}

	disabled := httpauth.HTTPAuthConfig{Disabled: true}
	if err := disabled.Validate(); err != nil {
		t.Fatalf("disabled config: %v", err)
	}

	tests := []struct {
		name    string
		cfg     httpauth.HTTPAuthConfig
		contain string
	}{
		{
			name:    "missing api key",
			cfg:     httpauth.HTTPAuthConfig{},
			contain: "api key is required",
		},
		{
			name:    "short api key",
			cfg:     httpauth.HTTPAuthConfig{APIKey: "short"},
			contain: "at least 16 bytes",
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
