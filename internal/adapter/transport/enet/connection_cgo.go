//go:build enet && cgo

package enet

import (
	"io"
	"sync"
	"sync/atomic"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

// EnetConnection - transport.Connection on top of a libenet peer. All libenet
// peer calls go as commands into the queue of the owning host's HostLoop
// (hostloop_cgo.go): consumers of transport.Connection never see libenet;
// there is no global serialization - only per-host.
//
// Send/Close - async enqueue: they need no result, and the FIFO queue
// preserves the wire order of frames and the final DisconnectLater. Ping -
// blocking Call: invoked from room/policy goroutines, the result is needed
// synchronously, and waiting is safe - the executor runs only time-bounded
// libenet calls and never waits on domain goroutines (see the invariant in
// hostloop_cgo.go).
type EnetConnection struct {
	loop      *HostLoop
	peer      libenet.Peer
	incoming  chan domain.Frame
	closed    chan struct{}
	closeOnce sync.Once
	alive     atomic.Bool
}

func NewEnetConnection(loop *HostLoop, peer libenet.Peer, incoming chan domain.Frame) *EnetConnection {
	c := &EnetConnection{
		loop:     loop,
		peer:     peer,
		incoming: incoming,
		closed:   make(chan struct{}),
	}
	c.alive.Store(true)
	return c
}

func (c *EnetConnection) Send(frame domain.Frame) error {
	if !c.alive.Load() {
		return io.EOF
	}
	if c.loop == nil {
		return io.EOF
	}
	payload := append([]byte{byte(frame.OpCode)}, frame.Payload...)
	return c.loop.enqueue(func() {
		// go-enet quirk: SendBytes → SendPacket ignores the return code of
		// enet_peer_send - sending to a disconnecting/zombie peer silently
		// fails (nil error). The safeguard is the alive flag above; the break
		// will surface as EventDisconnect → DisconnectPeer on the host loop side.
		_ = c.peer.SendBytes(payload, 0, packetFlagsFromDelivery(frame.Delivery))
	})
}

func (c *EnetConnection) Receive() (domain.Frame, error) {
	select {
	case <-c.closed:
		return domain.Frame{}, io.EOF
	case frame := <-c.incoming:
		return frame, nil
	}
}

func (c *EnetConnection) Ping() int64 {
	if c.peer == nil || c.loop == nil || !c.alive.Load() {
		return -1
	}
	rtt, err := Call(c.loop, func() int64 { return int64(c.peer.GetRoundTripTime()) })
	if err != nil {
		return -1
	}
	return rtt
}

func (c *EnetConnection) Close() error {
	c.closeOnce.Do(func() {
		c.alive.Store(false)
		// incoming is not closed: peerSession.deliver sends from the host loop outside our lifecycle → send-on-closed race (ADR 0003).
		close(c.closed)
		if c.peer != nil && c.loop != nil {
			// Graceful disconnect: DisconnectLater waits until queued outgoing
			// packets (join reject, final state frames) go out on the wire, and
			// only then sends disconnect. DisconnectNow would reset the peer and
			// drop the queue; plain Disconnect also does not guarantee delivery
			// of commands already queued.
			_ = c.loop.enqueue(func() { c.peer.DisconnectLater(0) })
		}
	})
	return nil
}

// IsAlive - false after Close(); on ENet zombies are reaped by the transport's own timeout (EventDisconnect → DisconnectPeer).
func (c *EnetConnection) IsAlive() bool {
	return c.alive.Load()
}
