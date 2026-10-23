//go:build !enet || !cgo

package enet

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
	portenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/enet"
)

type EnetConnectionFactory struct {
	logger logging.Logger
}

// NewEnetConnectionFactory - та же сигнатура, что и в cgo-ветке: *HostLoop
// принимается в конструкторе (stub-тип, hostloop_stub.go), в stub-сборке
// игнорируется.
func NewEnetConnectionFactory(_ *HostLoop, logger logging.Logger) *EnetConnectionFactory {
	return &EnetConnectionFactory{
		logger: logger,
	}
}

// WithLoop - stub-аналог cgo-ветки: возвращает фабрику без изменений
// (HostLoop в stub-сборке не существует, привязки нет).
func (f *EnetConnectionFactory) WithLoop(_ *HostLoop) *EnetConnectionFactory {
	return f
}

// CreateConnection — та же сигнатура, что и в cgo-ветке (Peer = any под stub-тегами);
// аргументы игнорируются: реальный peer существует только в enet+cgo сборке.
func (f *EnetConnectionFactory) CreateConnection(_ portenet.Peer, _ chan domain.Frame) transport.Connection {
	return NewEnetConnection()
}
