//go:build enet && cgo

package integration_test

import (
	"encoding/base64"
	"sync/atomic"
	"testing"
	"time"

	adapterenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
	libenet "github.com/codecat/go-enet"
)

// ENet variants of the "live match" e2e (plan docs/plans/P12-e2e-enet-mixed.md),
// ~115s of real time each:
//
//	TestE2E_LiveMatch_ENet  — 4 ENet clients; dropout = honest libenet silence
//	                          (stop host.Service → no acks → server-side peer
//	                          timeout (measured ~10s on localhost, depends on the
//	                          rate of outgoing reliable traffic) → EventDisconnect →
//	                          LeaveRoom → migration). Eviction wait bound — 70s.
//	TestE2E_LiveMatch_Mixed — host on ENet (same timeout drop), a second ENet
//	                          + 2 WS slaves: cross-transport coverage of
//	                          entities/input/migrations over the shared wire protocol.
//
// The shared client layer and scenario driver — match_e2e_client_test.go;
// the WS variant — match_e2e_test.go.

var _ matchClient = (*enetClient)(nil)

// enetClient - a room client on an in-test libenet host. The host is owned by
// adapterenet.HostLoop (P15): the pump drives Service via commands through its
// queue, other libenet calls go via Call/Do into the same loop. Per-host
// serialization: the P12 convoy on a shared mutex is gone, Service(1) remains
// for the pump's pacing.
type enetClient struct {
	matchClientBase
	loop     *adapterenet.HostLoop
	enetPeer libenet.Peer
	token    []byte // admit: the first packet after EventConnect — raw (decoded) ticket

	connected atomic.Bool // EventConnect received - handshake done
	pumpStop  atomic.Bool // stop(): the pump exits without waiting for EventDisconnect
}

// port - the ticket's assigned ENet port (P18 strict binding: the ticket is
// bound to a specific host; the client must connect exactly there).
func newEnetClient(t *testing.T, e *env.Env, nick, token string, port uint16) *enetClient {
	t.Helper()

	// The admit packet carries the raw ticket: JoinRoom expects a binary token
	// (the WS branch also base64-decodes the query parameter before JoinRoom).
	ticket, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("[%s] decode ticket token: %v", nick, err)
	}

	loop, err := adapterenet.NewHostLoop(nil, 1, 2, nil)
	if err != nil {
		t.Fatalf("[%s] enet host: %v", nick, err)
	}
	c := &enetClient{
		matchClientBase: newMatchClientBase(t, nick),
		token:           ticket,
		loop:            loop,
	}
	// Insurance against a t.Fatal before defer stop(): Stop is idempotent
	// (drain + host.Destroy + wait for the loop goroutine exit).
	t.Cleanup(loop.Stop)

	err = loop.Do(func() error {
		var err error
		c.enetPeer, err = loop.Host().Connect(libenet.NewAddress("127.0.0.1", port), 2, 0)
		return err
	})
	if err != nil {
		t.Fatalf("[%s] enet connect: %v", nick, err)
	}

	go c.eventLoop()
	go c.sendLoop(c.writeFrame)
	go c.statsLoop()

	// Wait for EventConnect before returning: this serializes the client
	// handshakes — parallel CONNECTs occasionally lose datagrams/replies and get
	// rescued by libenet retries with exponential backoff (~6s observed).
	waitFor(t, 15*time.Second, nick+" enet connect", func() bool {
		return c.connected.Load()
	})
	return c
}

// eventLoop - the client's pump: Service(1) + event dispatch. The admit frame
// (raw ticket) is sent on EventConnect; subsequent OpCode+payload frames go to
// the shared handleFrame. Exit — EventDisconnect (any reason) or pumpStop/silent.
func (c *enetClient) eventLoop() {
	defer close(c.pumpDone)
	admitted := false
	for {
		if c.silent.Load() {
			c.connDead.Store(true)
			c.t.Logf("[%s] service stopped — silent mode, UDP socket stays bound", c.nick)
			return
		}
		if c.pumpStop.Load() {
			return
		}
		// Service(1) as a command into our own HostLoop: the pump drives the
		// client service itself; the per-host queue means the server loop and
		// neighbor clients don't throttle it.
		ev, err := adapterenet.Call(c.loop, func() libenet.Event { return c.loop.Host().Service(1) })
		if err != nil {
			c.connDead.Store(true)
			return
		}
		switch ev.GetType() {
		case libenet.EventNone:
			continue
		case libenet.EventConnect:
			c.connected.Store(true)
			if admitted {
				continue
			}
			admitted = true
			err := c.loop.Do(func() error {
				return c.enetPeer.SendBytes(c.token, 0, libenet.PacketFlagReliable)
			})
			if err != nil {
				c.connDead.Store(true)
				c.t.Logf("[%s] admit send failed: %v", c.nick, err)
				return
			}
			c.t.Logf("[%s] enet connected, admit sent", c.nick)
		case libenet.EventReceive:
			data, err := adapterenet.Call(c.loop, func() []byte { return ev.GetPacket().GetData() })
			if err != nil {
				c.connDead.Store(true)
				return
			}
			_ = c.loop.Do(func() error { ev.GetPacket().Destroy(); return nil })
			if len(data) == 0 {
				continue
			}
			c.handleFrame(domain.OpCode(data[0]), data[1:])
		case libenet.EventDisconnect:
			c.connDead.Store(true)
			switch {
			case c.leftClean.Load():
				c.t.Logf("[%s] enet disconnected (planned)", c.nick)
			case c.silent.Load():
				c.t.Logf("[%s] enet disconnected while silent", c.nick)
			default:
				c.t.Logf("[%s] enet disconnected (data=%d)", c.nick, ev.GetData())
			}
			return
		}
	}
}

// writeFrame — OpCode+payload on channel 0 reliable — same as the server sends
// (EnetConnection.Send → packetFlagsFromDelivery → PacketFlagReliable).
func (c *enetClient) writeFrame(op domain.OpCode, payload []byte) error {
	frame := append([]byte{byte(op)}, payload...)
	return c.loop.Do(func() error {
		return c.enetPeer.SendBytes(frame, 0, libenet.PacketFlagReliable)
	})
}

// goSilent — an honest dropout: the event loop stops calling host.Service →
// the client sends no acks/pings, the UDP socket stays open. The server-side
// libenet detects the dead peer via its own timeout (~5–30s) → EventDisconnect →
// LeaveRoom → migration. NOT peer.DisconnectNow — that would be a clean-ish drop.
func (c *enetClient) goSilent() {
	c.silent.Store(true)
	c.stopSend()
}

// leaveClean — the scheduled departure of master #2: a graceful
// peer.Disconnect — the server gets the disconnect command → EventDisconnect →
// LeaveRoom. The pump keeps servicing until its own EventDisconnect.
func (c *enetClient) leaveClean() {
	c.leftClean.Store(true)
	c.stopSend()
	_ = c.loop.Do(func() error { c.enetPeer.Disconnect(0); return nil })
}

// stop - final teardown: graceful disconnect (if the pump is still alive), a
// bound on the pump's exit, then loop.Stop → host.Destroy. Destroy only after
// pumpDone - a Service on a destroyed host would be UB.
func (c *enetClient) stop() {
	c.leftClean.Store(true)
	c.stopSend()
	select {
	case <-c.pumpDone:
	default:
		_ = c.loop.Do(func() error { c.enetPeer.Disconnect(0); return nil })
	}
	select {
	case <-c.pumpDone:
	case <-time.After(3 * time.Second):
		c.pumpStop.Store(true)
		<-c.pumpDone
	}
	<-c.sendDone
	c.loop.Stop()
}

// TestE2E_LiveMatch_ENet — 4 ENet clients, host dropout = libenet silence.
func TestE2E_LiveMatch_ENet(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e live match ~115s real time — skipped under -short")
	}
	t.Parallel()

	e := env.New(t, env.WithEnet())

	roomID, hostToken, hostPort := e.CreateRoomPort(t, "host")
	host := newEnetClient(t, e, "host", hostToken, hostPort)
	token1, port1 := e.IssueTicketPort(t, roomID, "e1")
	s1 := newEnetClient(t, e, "e1", token1, port1)
	token2, port2 := e.IssueTicketPort(t, roomID, "e2")
	s2 := newEnetClient(t, e, "e2", token2, port2)
	token3, port3 := e.IssueTicketPort(t, roomID, "e3")
	s3 := newEnetClient(t, e, "e3", token3, port3)
	clients := []*enetClient{host, s1, s2, s3}
	defer func() {
		for _, c := range clients {
			c.stop()
		}
	}()

	runLiveMatch(t, e, roomID, host, []matchClient{s1, s2, s3}, liveMatchTiming{
		hostSilentAt:  15 * time.Second,
		evictBound:    70 * time.Second, // libenet peer timeout: ~10s measured, bound with CI headroom
		secondLeaveAt: 95 * time.Second,
		finishAt:      115 * time.Second,
	})
}

// TestE2E_LiveMatch_Mixed — a host on ENet (timeout drop) + ENet and WS slaves
// in one room: survivors on both transports see the migrations, entities and
// each other's inputs over the shared wire protocol.
func TestE2E_LiveMatch_Mixed(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e live match ~115s real time — skipped under -short")
	}
	t.Parallel()

	e := env.New(t, env.WithEnet())

	roomID, hostToken, hostPort := e.CreateRoomPort(t, "host")
	// The host — first joiner on ENet → initial master; dropout — enet timeout.
	host := newEnetClient(t, e, "host", hostToken, hostPort)
	token1, port1 := e.IssueTicketPort(t, roomID, "e1")
	e1 := newEnetClient(t, e, "e1", token1, port1)
	w1 := newWSClient(t, e, "w1", e.IssueTicket(t, roomID, "w1"))
	w2 := newWSClient(t, e, "w2", e.IssueTicket(t, roomID, "w2"))
	slaves := []matchClient{e1, w1, w2}
	defer func() {
		host.stop()
		for _, s := range slaves {
			s.stop()
		}
	}()

	runLiveMatch(t, e, roomID, host, slaves, liveMatchTiming{
		hostSilentAt:  15 * time.Second,
		evictBound:    70 * time.Second,
		secondLeaveAt: 95 * time.Second,
		finishAt:      115 * time.Second,
	})
}
