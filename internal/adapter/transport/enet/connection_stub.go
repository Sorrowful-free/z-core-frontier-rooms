//go:build !enet || !cgo

package enet

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type EnetConnection struct {
	incoming chan domain.Frame
}

func (c *EnetConnection) Close() error {
	return nil
}

func (c *EnetConnection) GetIncoming() chan<- domain.Frame {
	return c.incoming
}

func (c *EnetConnection) Send(frame domain.Frame) error {
	return nil
}
