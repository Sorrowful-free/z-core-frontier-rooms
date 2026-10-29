//go:build enet && cgo

package enet_test

import (
	"context"
	"encoding/base64"
	"slices"
	"testing"
	"time"

	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
	adapterenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
	libenet "github.com/codecat/go-enet"
)

// P37: an inbound-queue overflow on a reliable frame escalates to teardown
// (graceful disconnect → DisconnectPeer when admitted → grace); unreliable
// frames may still be dropped.
//
// Both tests run on rendezvous queues (size 0): sess.incoming and
// room.incoming deliver only into a parked receiver, so a burst of inbound
// packets overflows somewhere on the path. The observable contract is the
// same at either overflow point — reliable → disconnect, unreliable → drop —
// so the tests do not pin down which queue overflowed first.

func newOverflowEnv(t *testing.T) (*env.Env, domain.RoomID, []byte, uint16) {
	t.Helper()

	e := env.New(t,
		env.WithEnet(),
		env.WithTransport(adaptertransport.TransportConfig{
			MaxIncomingFrameBytes: adaptertransport.DefaultMaxIncomingFrameBytes,
			RoomIncomingQueueSize: 0,
			// A large outbound buffer: delivered flood frames answer with
			// in-room error replies — they must not overflow the outbound
			// queue (a reliable frame there would close the conn too, P37).
			PeerOutboundQueueSize: 1024,
			EnetIncomingQueueSize: 0,
			WSWriteDeadline:       adaptertransport.DefaultWSWriteDeadline,
			PeerCloseDrainTimeout: adaptertransport.DefaultPeerCloseDrainTimeout,
		}),
	)
	roomID, token, port := e.CreateRoomPort(t, "host")
	ticket, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("decode ticket: %v", err)
	}
	return e, roomID, ticket, port
}

// pumpFor services the client host for a while — the test client has no
// background pump (waitEvent* is the pump), so a queued packet only reaches
// the wire while Service runs.
func (c *enetTestClient) pumpFor(t *testing.T, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		ev, err := adapterenet.Call(c.loop, func() libenet.Event { return c.loop.Host().Service(10) })
		if err != nil {
			t.Fatalf("client service: %v", err)
		}
		if ev.GetType() == libenet.EventReceive {
			_ = c.loop.Do(func() error { ev.GetPacket().Destroy(); return nil })
		}
	}
}

// admitEnetClient connects, sends the raw ticket as the first packet and
// waits until the peer is visible in the room (admit done, the peer's read
// loop is draining sess.incoming).
func admitEnetClient(t *testing.T, e *env.Env, roomID domain.RoomID, ticket []byte, port uint16, nick string) *enetTestClient {
	t.Helper()

	client := newEnetTestClient(t, port)
	client.waitEvent(t, libenet.EventConnect, 3*time.Second)
	if err := client.loop.Do(func() error {
		return client.peer.SendBytes(ticket, 0, libenet.PacketFlagReliable)
	}); err != nil {
		t.Fatalf("send admit packet: %v", err)
	}
	// Flush the admit packet and wait for the join to land — retransmits
	// only happen while the client host is serviced, so pump and poll in
	// turns.
	for range 15 {
		client.pumpFor(t, 200*time.Millisecond)
		if room, err := e.Registry.GetRoom(context.Background(), roomID); err == nil {
			for _, p := range room.GetPeers() {
				if p.GetNickName() == nick {
					return client
				}
			}
		}
	}
	// Unreachable in practice — produces the standard poll failure text.
	e.WaitPeerNick(t, roomID, nick, true)
	return client
}

// waitEventPoll — waitEvent variant for overflow tests: skippable event
// types (EventReceive replies, EventNone) are ignored (received packets are
// destroyed), timeout returns nil instead of failing — the caller decides
// whether absence of the event is a failure.
func (c *enetTestClient) waitEventPoll(t *testing.T, want libenet.EventType, timeout time.Duration, skip ...libenet.EventType) libenet.Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ev, err := adapterenet.Call(c.loop, func() libenet.Event { return c.loop.Host().Service(10) })
		if err != nil {
			t.Fatalf("client service: %v", err)
		}
		typ := ev.GetType()
		if typ == want {
			return ev
		}
		if typ == libenet.EventReceive {
			_ = c.loop.Do(func() error { ev.GetPacket().Destroy(); return nil })
		}
		if typ != libenet.EventNone && !slices.Contains(skip, typ) {
			t.Fatalf("unexpected event %v, want %v", typ, want)
		}
	}
	return nil
}

// sendBurst queues n one-byte frames (a valid but unknown opcode — each
// delivered frame answers with an in-room error reply) on the wire at once.
func (c *enetTestClient) sendBurst(t *testing.T, n int, flags libenet.PacketFlags) {
	t.Helper()
	if err := c.loop.Do(func() error {
		for range n {
			if err := c.peer.SendBytes([]byte{0x99}, 0, flags); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("send burst: %v", err)
	}
}

// A reliable frame is never silently dropped: some packet of a burst hits a
// full queue and the session is torn down — the client sees the graceful
// EventDisconnect (in-room error replies in flight are skipped).
func TestEnetIncoming_ReliableOverflowDisconnects(t *testing.T) {
	e, roomID, ticket, port := newOverflowEnv(t)
	client := admitEnetClient(t, e, roomID, ticket, port, "host")

	// Retried bursts: whether a single packet overflows is a rendezvous
	// race against the drain loops; 10 bursts of 200 make a never-overflow
	// outcome practically impossible.
	for attempt := range 10 {
		client.sendBurst(t, 200, libenet.PacketFlagReliable)
		if ev := client.waitEventPoll(t, libenet.EventDisconnect, 500*time.Millisecond, libenet.EventReceive); ev != nil {
			data, err := adapterenet.Call(client.loop, func() uint32 { return ev.GetData() })
			if err != nil {
				t.Fatalf("event get data: %v", err)
			}
			t.Logf("disconnected after burst %d (data=%d)", attempt+1, data)
			return
		}
	}
	t.Fatal("a reliable flood on full queues must escalate to disconnect")
}

// An unreliable frame may still be dropped on a full queue — the connection
// must survive the flood: a later reliable probe still gets a server reply.
func TestEnetIncoming_UnreliableOverflowConnSurvives(t *testing.T) {
	e, roomID, ticket, port := newOverflowEnv(t)
	client := admitEnetClient(t, e, roomID, ticket, port, "host")

	client.sendBurst(t, 500, 0) // flags=0 → DeliveryUnreliable on the server

	// Liveness probe: an unknown-opcode frame answers with an in-room error
	// (0x66) — any EventReceive after the flood proves the conn survived.
	// An EventDisconnect while waiting fails inside waitEventPoll.
	if err := client.loop.Do(func() error {
		return client.peer.SendBytes([]byte{0x99}, 0, libenet.PacketFlagReliable)
	}); err != nil {
		t.Fatalf("send probe: %v", err)
	}
	ev := client.waitEventPoll(t, libenet.EventReceive, 5*time.Second)
	if ev == nil {
		t.Fatal("no server reply after unreliable flood — connection did not survive")
	}
	data, err := adapterenet.Call(client.loop, func() []byte { return ev.GetPacket().GetData() })
	if err != nil {
		t.Fatalf("packet get data: %v", err)
	}
	_ = client.loop.Do(func() error { ev.GetPacket().Destroy(); return nil })
	if len(data) == 0 {
		t.Fatal("empty server frame after unreliable flood")
	}
}
