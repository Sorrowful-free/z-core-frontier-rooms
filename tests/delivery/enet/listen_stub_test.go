//go:build !enet || !cgo

package enet_test

import (
	"context"
	"testing"
	"time"

	adapterenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	deliveryenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/enet"
)

type discardLogger struct{}

func (discardLogger) Error(string, ...any) {}
func (discardLogger) Info(string, ...any)  {}
func (discardLogger) Debug(string, ...any) {}
func (discardLogger) Warn(string, ...any)  {}
func (discardLogger) Fatal(string, ...any) {}

// Stub build: Listen возвращает handle, host не поднимается — Done закрывается,
// Err() == nil (listener disabled, не failure), Stop не блокируется.
func TestListenStubExitsClean(t *testing.T) {
	t.Parallel()

	h := deliveryenet.NewRoomsHandler(context.Background(), nil, nil, nil, adapterenet.DefaultEnetConfig(), discardLogger{})
	handle := h.Listen()

	select {
	case <-handle.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("stub listen did not exit")
	}
	if err := handle.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil (disabled listener)", err)
	}
	handle.Stop()
}
