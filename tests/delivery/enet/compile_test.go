//go:build enet && cgo

package enet_test

import (
	"testing"

	adapterenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
)

type discardLogger struct{}

func (discardLogger) Error(string, ...any) {}
func (discardLogger) Info(string, ...any)  {}
func (discardLogger) Debug(string, ...any) {}
func (discardLogger) Warn(string, ...any)  {}
func (discardLogger) Fatal(string, ...any) {}

// Smoke: линкует cgo-пакеты adapter/delivery enet (go-enet) при go test -tags enet.
func TestEnetPackagesCompile(t *testing.T) {
	t.Parallel()

	cfg := adapterenet.DefaultEnetConfig()
	if len(cfg.ListenPorts) != 1 || cfg.ListenPorts[0] != 7777 {
		t.Fatalf("ListenPorts = %v, want [7777]", cfg.ListenPorts)
	}

	factory := adapterenet.NewEnetConnectionFactory(nil, discardLogger{})
	if factory == nil {
		t.Fatal("NewEnetConnectionFactory returned nil")
	}
}
