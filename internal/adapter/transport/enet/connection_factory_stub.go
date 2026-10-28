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

// NewEnetConnectionFactory - same signature as the cgo branch: *HostLoop is
// taken in the constructor (stub type, hostloop_stub.go) and ignored in the
// stub build.
func NewEnetConnectionFactory(_ *HostLoop, logger logging.Logger) *EnetConnectionFactory {
	return &EnetConnectionFactory{
		logger: logger,
	}
}

// WithLoop - stub counterpart of the cgo branch: returns the factory
// unchanged (no HostLoop exists in the stub build, no binding).
func (f *EnetConnectionFactory) WithLoop(_ *HostLoop) *EnetConnectionFactory {
	return f
}

// CreateConnection — same signature as the cgo branch (Peer = any under stub
// tags); arguments are ignored: a real peer exists only in the enet+cgo build.
func (f *EnetConnectionFactory) CreateConnection(_ portenet.Peer, _ chan domain.Frame) transport.Connection {
	return NewEnetConnection()
}
