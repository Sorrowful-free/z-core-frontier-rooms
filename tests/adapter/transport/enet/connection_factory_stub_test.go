//go:build !enet || !cgo

package enet_test

import (
	"testing"

	adapterenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	portenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/enet"
)

// Compile-assertion: stub-реализация удовлетворяет тому же untagged port-интерфейсу
// (portenet.Peer = any без enet/cgo) — единая сигнатура CreateConnection(peer, incoming).
var _ portenet.EnetConnectionFactory = (*adapterenet.EnetConnectionFactory)(nil)

type discardLogger struct{}

func (discardLogger) Error(string, ...any) {}
func (discardLogger) Info(string, ...any)  {}
func (discardLogger) Debug(string, ...any) {}
func (discardLogger) Warn(string, ...any)  {}
func (discardLogger) Fatal(string, ...any) {}

// Stub-ветка: CreateConnection игнорирует аргументы и отдаёт no-op connection.
func TestEnetConnectionFactoryStubCreateConnection(t *testing.T) {
	t.Parallel()

	factory := adapterenet.NewEnetConnectionFactory(nil, discardLogger{})
	conn := factory.CreateConnection(nil, make(chan domain.Frame, 1))
	if conn == nil {
		t.Fatal("CreateConnection returned nil")
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
}
