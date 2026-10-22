package server_test

import (
	"testing"

	adapterserver "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/server"
)

func TestServerConfig_Validate(t *testing.T) {
	t.Parallel()

	if err := (adapterserver.ServerConfig{HTTPAddr: adapterserver.DefaultHTTPAddr}).Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	if err := (adapterserver.ServerConfig{}).Validate(); err == nil {
		t.Fatal("empty HTTPAddr: expected error")
	}
}
