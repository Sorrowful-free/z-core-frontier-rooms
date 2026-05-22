//go:build !enet || !cgo

package enet

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

type EnetConnectionFactory struct {
	logger logging.Logger
}

func NewEnetConnectionFactory(logger logging.Logger) *EnetConnectionFactory {
	return &EnetConnectionFactory{
		logger: logger,
	}
}

func (f *EnetConnectionFactory) CreateConnection() transport.Connection {
	return NewEnetConnection()
}
