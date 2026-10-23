//go:build enet && cgo

package enet

import (
	"sync/atomic"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
	portenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/enet"
)

type EnetConnectionFactory struct {
	logger logging.Logger
	loop   atomic.Pointer[HostLoop]
}

// NewEnetConnectionFactory принимает HostLoop в конструкторе (peer→host
// привязка: у libenet.Peer нет GetHost). На серверном пути loop создаётся
// внутри delivery при старте listener'а - позже конструктора фабрики - поэтому
// допустимо nil + BindLoop до первого accept'а.
func NewEnetConnectionFactory(loop *HostLoop, logger logging.Logger) *EnetConnectionFactory {
	f := &EnetConnectionFactory{logger: logger}
	if loop != nil {
		f.loop.Store(loop)
	}
	return f
}

// BindLoop привязывает фабрику к HostLoop после его создания (delivery/enet
// вызывает один раз при старте host loop'а, до accept'а peer'ов).
func (f *EnetConnectionFactory) BindLoop(loop *HostLoop) {
	f.loop.Store(loop)
}

// CreateConnection - под enet+cgo portenet.Peer разворачивается в libenet.Peer
// (tagged alias), поэтому сигнатура совпадает с stub-веткой и остаётся типобезопасной.
func (f *EnetConnectionFactory) CreateConnection(peer portenet.Peer, incoming chan domain.Frame) transport.Connection {
	return NewEnetConnection(f.loop.Load(), peer, incoming)
}
