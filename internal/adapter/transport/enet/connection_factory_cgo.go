//go:build enet && cgo

package enet

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

type EnetConnectionFactory struct {
}

func NewEnetConnectionFactory(logger logging.Logger) *EnetConnectionFactory {
	return &EnetConnectionFactory{
		logger: logger,
	}
}

func (f *EnetConnectionFactory) CreateConnection(peer libenet.Peer, incoming chan domain.Frame) transport.Connection {
	return NewEnetConnection(peer, incoming)
}
