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

// NewEnetConnectionFactory takes a HostLoop in the constructor (peer→host
// binding: libenet.Peer has no GetHost). On the server path hosts (P18: one
// per port) are created inside delivery at listener startup - later than the
// factory constructor - so nil + WithLoop per host before accepting peers is
// allowed.
func NewEnetConnectionFactory(loop *HostLoop, logger logging.Logger) *EnetConnectionFactory {
	f := &EnetConnectionFactory{logger: logger}
	if loop != nil {
		f.loop.Store(loop)
	}
	return f
}

// WithLoop returns a factory bound to the given HostLoop: delivery/enet calls
// it once per host when starting its run goroutine. The binding is immutable
// from the outside - a shared BindLoop across several hosts would route one
// host's conns into another loop's queue (libenet confinement violation).
func (f *EnetConnectionFactory) WithLoop(loop *HostLoop) *EnetConnectionFactory {
	return NewEnetConnectionFactory(loop, f.logger)
}

// CreateConnection - under enet+cgo portenet.Peer expands to libenet.Peer
// (tagged alias), so the signature matches the stub branch and stays type-safe.
func (f *EnetConnectionFactory) CreateConnection(peer portenet.Peer, incoming chan domain.Frame) transport.Connection {
	return NewEnetConnection(f.loop.Load(), peer, incoming)
}
