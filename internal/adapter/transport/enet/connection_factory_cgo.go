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
// привязка: у libenet.Peer нет GetHost). На серверном пути host'ы (P18: один
// на порт) создаются внутри delivery при старте listener'а - позже конструктора
// фабрики - поэтому допустимо nil + WithLoop на каждый host до accept'а peer'ов.
func NewEnetConnectionFactory(loop *HostLoop, logger logging.Logger) *EnetConnectionFactory {
	f := &EnetConnectionFactory{logger: logger}
	if loop != nil {
		f.loop.Store(loop)
	}
	return f
}

// WithLoop возвращает фабрику, привязанную к данному HostLoop: delivery/enet
// вызывает её один раз на host при старте его run-горутины. Привязка иммутабельна
// снаружи - shared BindLoop на нескольких host'ах направлял бы conn'ы одного
// host'а в очередь чужого loop'а (нарушение confinement libenet).
func (f *EnetConnectionFactory) WithLoop(loop *HostLoop) *EnetConnectionFactory {
	return NewEnetConnectionFactory(loop, f.logger)
}

// CreateConnection - под enet+cgo portenet.Peer разворачивается в libenet.Peer
// (tagged alias), поэтому сигнатура совпадает с stub-веткой и остаётся типобезопасной.
func (f *EnetConnectionFactory) CreateConnection(peer portenet.Peer, incoming chan domain.Frame) transport.Connection {
	return NewEnetConnection(f.loop.Load(), peer, incoming)
}
