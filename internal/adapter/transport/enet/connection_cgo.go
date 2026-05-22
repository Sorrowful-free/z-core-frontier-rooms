//go:build enet && cgo

package enet

import (
	"io"
	"sync"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

type EnetConnection struct {
	peer      libenet.Peer
	incoming  chan []byte
	closeOnce sync.Once
}

func NewEnetConnection(peer libenet.Peer, incoming chan domain.Frame) *EnetConnection {
	return &EnetConnection{
		peer:     peer,
		incoming: incoming,
	}
}

func (c *EnetConnection) Send(frame domain.Frame) error {
	payload := append([]byte{byte(frame.OpCode)}, frame.Payload...)
	return c.peer.SendBytes(payload, 0, libenet.PacketFlagReliable)
}

func (c *EnetConnection) Receive() (domain.Frame, error) {
	frame, ok := <-c.incoming
	if !ok {
		return domain.Frame{}, io.EOF
	}
	return frame, nil
}

func (c *EnetConnection) Close() error {
	c.closeOnce.Do(func() {
		if c.incoming != nil {
			close(c.incoming)
		}
		if c.peer != nil {
			c.peer.DisconnectNow(0)
		}
	})
	return nil
}
