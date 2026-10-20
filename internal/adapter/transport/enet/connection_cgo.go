//go:build enet && cgo

package enet

import (
	"io"
	"sync"
	"sync/atomic"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

type EnetConnection struct {
	peer      libenet.Peer
	incoming  chan domain.Frame
	closeOnce sync.Once
	alive     atomic.Bool
}

func NewEnetConnection(peer libenet.Peer, incoming chan domain.Frame) *EnetConnection {
	c := &EnetConnection{
		peer:     peer,
		incoming: incoming,
	}
	c.alive.Store(true)
	return c
}

func (c *EnetConnection) Send(frame domain.Frame) error {
	payload := append([]byte{byte(frame.OpCode)}, frame.Payload...)
	return c.peer.SendBytes(payload, 0, packetFlagsFromDelivery(frame.Delivery))
}

func (c *EnetConnection) Receive() (domain.Frame, error) {
	frame, ok := <-c.incoming
	if !ok {
		return domain.Frame{}, io.EOF
	}
	return frame, nil
}

func (c *EnetConnection) Ping() int64 {
	if c.peer == nil {
		return -1
	}
	return int64(c.peer.GetRoundTripTime())
}

func (c *EnetConnection) Close() error {
	c.closeOnce.Do(func() {
		c.alive.Store(false)
		if c.incoming != nil {
			close(c.incoming)
		}
		if c.peer != nil {
			c.peer.DisconnectNow(0)
		}
	})
	return nil
}

// IsAlive — false после Close(); zombie на ENet вычищает собственный таймаут транспорта (EventDisconnect → LeaveRoom).
func (c *EnetConnection) IsAlive() bool {
	return c.alive.Load()
}
