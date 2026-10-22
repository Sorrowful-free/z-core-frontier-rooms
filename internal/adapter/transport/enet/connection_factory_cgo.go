//go:build enet && cgo

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

func NewEnetConnectionFactory(logger logging.Logger) *EnetConnectionFactory {
	return &EnetConnectionFactory{
		logger: logger,
	}
}

// CreateConnection — под enet+cgo portenet.Peer разворачивается в libenet.Peer
// (tagged alias), поэтому сигнатура совпадает с stub-веткой и остаётся типобезопасной.
func (f *EnetConnectionFactory) CreateConnection(peer portenet.Peer, incoming chan domain.Frame) transport.Connection {
	return NewEnetConnection(peer, incoming)
}
