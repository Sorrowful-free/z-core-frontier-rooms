//go:build enet && cgo

package enet

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/codecat/go-enet"
)

type EnetConnection struct {
	conn     *enet.Host
	peer     *enet.Peer
	incoming chan domain.Frame
}

func (c *EnetConnection) Close() error {
	return c.conn.Close()
}

func (c *EnetConnection) GetIncoming() chan<- domain.Frame {
	return c.incoming
}

func (c *EnetConnection) Send(frame domain.Frame) error {
	return c.conn.Send(frame.Payload)
}
