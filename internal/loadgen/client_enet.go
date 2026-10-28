//go:build enet && cgo

package loadgen

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sync"

	adapterenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

// enetClientFramesBuf — buffer of incoming frames pump→Recv. The reference
// point is the server side's TRANSPORT_ENET_INCOMING_QUEUE; overflow =
// honest backpressure on the pump (Service stalls → server-side peer
// timeout), not a drop.
const enetClientFramesBuf = 1024

// enetDataConn — DataConn over a client libenet host (P27 inc3).
// The host is owned by adapterenet.HostLoop — ALL libenet calls go as
// commands through Call/Do (per-host serialization, same as the server side
// and the e2e enetClient in tests/integration/match_e2e_enet_test.go). The
// pump goroutine drives Service(1) and dispatches events: EventConnect →
// admit packet (raw decoded ticket, channel 0 reliable), EventReceive →
// frame into the frames channel, EventDisconnect → end of session
// (close(dead) → Recv EOF).
type enetDataConn struct {
	loop *adapterenet.HostLoop
	peer libenet.Peer

	frames    chan enetFrame
	connected chan struct{} // closed by pump on EventConnect (handshake + admit sent)
	dead      chan struct{} // closed by pump on exit — Recv returns an error
	err       error         // terminal cause; written before close(dead) → visible to the dead reader

	closeOnce sync.Once
}

type enetFrame struct {
	op      domain.OpCode
	payload []byte
}

// dialEnet — the ENet branch of dialDataConn: own host (peerLimit=1) →
// Connect to EnetAddr:EnetPort (port strictly from the ticket response, P18
// strict binding) → wait for EventConnect with the ctx deadline → the raw
// ticket goes as the admit packet.
// EnetPort==0 — the server was built/configured without enet listeners →
// dial error.
func dialEnet(ctx context.Context, p DialParams) (DataConn, error) {
	if p.EnetPort == 0 {
		return nil, errors.New("enet dial: ticket has no enet_port binding")
	}
	// Admit = the binary token as the first packet (the WS branch sends
	// base64url in the query and the server decodes it; on ENet the client
	// decodes).
	ticket, err := base64.RawURLEncoding.DecodeString(p.Token)
	if err != nil {
		return nil, fmt.Errorf("enet dial: decode ticket token: %w", err)
	}
	loop, err := adapterenet.NewHostLoop(nil, 1, 2, nil)
	if err != nil {
		return nil, fmt.Errorf("enet dial: host: %w", err)
	}
	c := &enetDataConn{
		loop:      loop,
		frames:    make(chan enetFrame, enetClientFramesBuf),
		connected: make(chan struct{}),
		dead:      make(chan struct{}),
	}
	if err := loop.Do(func() error {
		var err error
		c.peer, err = loop.Host().Connect(libenet.NewAddress(p.EnetAddr, p.EnetPort), 2, 0)
		return err
	}); err != nil {
		loop.Stop()
		return nil, fmt.Errorf("enet dial %s:%d: %w", p.EnetAddr, p.EnetPort, err)
	}
	go c.pump(ticket)
	select {
	case <-c.connected:
		return c, nil
	case <-c.dead:
		loop.Stop()
		if c.err != nil {
			return nil, fmt.Errorf("enet dial %s:%d: %w", p.EnetAddr, p.EnetPort, c.err)
		}
		return nil, fmt.Errorf("enet dial %s:%d: disconnected during handshake", p.EnetAddr, p.EnetPort)
	case <-ctx.Done():
		// The handshake didn't fit in the dial timeout: Stop is idempotent,
		// pump exits on its own via a Call error after the loop stops.
		loop.Stop()
		return nil, ctx.Err()
	}
}

// pump — the client service loop: Service(1) as a command in its own
// HostLoop + event dispatch. The first EventConnect carries the admit
// (ticket) on channel 0 reliable — mirroring the e2e enetClient. Exit:
// EventDisconnect (any reason) or a Call error after loop.Stop (Close).
func (c *enetDataConn) pump(ticket []byte) {
	defer close(c.dead)
	admitted := false
	for {
		ev, err := adapterenet.Call(c.loop, func() libenet.Event { return c.loop.Host().Service(1) })
		if err != nil {
			return
		}
		switch ev.GetType() {
		case libenet.EventNone:
		case libenet.EventConnect:
			if admitted {
				continue
			}
			admitted = true
			if err := c.loop.Do(func() error {
				return c.peer.SendBytes(ticket, 0, libenet.PacketFlagReliable)
			}); err != nil {
				c.err = fmt.Errorf("admit send: %w", err)
				return
			}
			close(c.connected)
		case libenet.EventReceive:
			data, err := adapterenet.Call(c.loop, func() []byte { return ev.GetPacket().GetData() })
			_ = c.loop.Do(func() error { ev.GetPacket().Destroy(); return nil })
			if err != nil {
				return
			}
			if len(data) == 0 {
				continue
			}
			c.frames <- enetFrame{op: domain.OpCode(data[0]), payload: data[1:]}
		case libenet.EventDisconnect:
			c.err = fmt.Errorf("enet disconnected (data=%d)", ev.GetData())
			return
		}
	}
}

// Send — a [op|payload] frame on channel 0 reliable: same as the server
// sends in EnetConnection.Send (packetFlagsFromDelivery →
// PacketFlagReliable).
func (c *enetDataConn) Send(op domain.OpCode, payload []byte) error {
	frame := append([]byte{byte(op)}, payload...)
	return c.loop.Do(func() error {
		return c.peer.SendBytes(frame, 0, libenet.PacketFlagReliable)
	})
}

// Recv — the next frame or a drop. After dead it drains the rest of the
// buffer, then returns the pump's terminal error (or EOF on a clean Close).
func (c *enetDataConn) Recv() (domain.OpCode, []byte, error) {
	select {
	case f := <-c.frames:
		return f.op, f.payload, nil
	case <-c.dead:
	}
	select {
	case f := <-c.frames:
		return f.op, f.payload, nil
	default:
	}
	if c.err != nil {
		return 0, nil, c.err
	}
	return 0, nil, io.EOF
}

// Close — abrupt drop: DisconnectNow (best-effort reset, the server sees a
// disconnect), then loop.Stop — queue drain + host.Destroy + waiting for the
// loop goroutine to exit. Idempotent (closeOnce + idempotent Stop).
func (c *enetDataConn) Close() error {
	c.closeOnce.Do(func() {
		_ = c.loop.Do(func() error { c.peer.DisconnectNow(0); return nil })
		c.loop.Stop()
	})
	return nil
}
