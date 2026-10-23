//go:build enet && cgo

package enet_test

import (
	"testing"

	adapterenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	portenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/enet"
)

// Compile-assertion: реализация удовлетворяет port-интерфейсу под enet+cgo,
// где portenet.Peer — libenet.Peer.
var _ portenet.EnetConnectionFactory = (*adapterenet.EnetConnectionFactory)(nil)

type discardLogger struct{}

func (discardLogger) Error(string, ...any) {}
func (discardLogger) Info(string, ...any)  {}
func (discardLogger) Debug(string, ...any) {}
func (discardLogger) Warn(string, ...any)  {}
func (discardLogger) Fatal(string, ...any) {}

// Поведенческий smoke cgo-ветки: nil-peer допустим (libenet.Peer — интерфейс);
// Send/Ping на nil-peer не трогают libenet (alive-флаг / nil-check в EnetConnection).
func TestEnetConnectionFactoryCreateConnection(t *testing.T) {
	t.Parallel()

	factory := adapterenet.NewEnetConnectionFactory(nil, discardLogger{})
	conn := factory.CreateConnection(nil, make(chan domain.Frame, 1))
	if conn == nil {
		t.Fatal("CreateConnection returned nil")
	}
	if !conn.IsAlive() {
		t.Fatal("new connection IsAlive() = false, want true")
	}
	if ping := conn.Ping(); ping != -1 {
		t.Fatalf("Ping() on nil peer = %d, want -1", ping)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	if conn.IsAlive() {
		t.Fatal("IsAlive() after Close = true, want false")
	}
}
