//go:build enet && cgo

package integration_test

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	adapterenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
	libenet "github.com/codecat/go-enet"
)

// E2E reconnect over the real ENet transport (plan docs/plans/P22-enet-reconnect-e2e.md,
// on top of the P20/P24 semantics and the WS coverage of reconnect_test.go):
//
//	TestE2E_ENetReconnect          — disconnect → 0x80 OpPeerDisconnected at the
//	                                host → resume with reconnect_token as the first
//	                                packet → 0x81 OpPeerReconnected at the host +
//	                                a full state resync for the returnee, nick
//	                                from the slot.
//	TestE2E_ENetReconnectExpired   — short ReconnectTTL: resume past grace →
//	                                0x74 OpReservationExpired (the P24 deadline
//	                                gate; the orphan sweep is not running in env →
//	                                the branch is deterministic, 0x73 is
//	                                unreachable).
//	TestE2E_ENetReconnectWrongHost — multi-host env: the reconnect token inherits
//	                                the join ticket's claims.hostID; a resume
//	                                packet on a foreign host → 0x77 OpWrongHost,
//	                                the slot is not consumed (a correct-host
//	                                resume afterwards succeeds).
//
// The client is the lightweight enetWireClient below: the same plumbing as
// enetClient from match_e2e_enet_test.go (HostLoop, Call/Do, admit as the
// first packet), but without the match machinery (input/entities streams are
// not needed) and with raw wire-frame recording — the shared client does not
// observe the 0x7x/0x8x opcode feed.

// wireFrame — a received server frame (OpCode + payload) in arrival order.
type wireFrame struct {
	op      domain.OpCode
	payload []byte
}

// enetWireClient — an in-test ENet client for reconnect scenarios: connect →
// admit as the first packet (an arbitrary raw token: join ticket or
// reconnect token) →
// Service events into the frames feed → graceful disconnect → stop.
type enetWireClient struct {
	t    *testing.T
	nick string

	loop     *adapterenet.HostLoop
	enetPeer libenet.Peer
	token    []byte // admit: the first packet after EventConnect — raw (decoded) token

	connected atomic.Bool // EventConnect received — handshake done
	connDead  atomic.Bool // pump saw a connection error/drop
	pumpStop  atomic.Bool // stop(): the pump exits without waiting for EventDisconnect
	leftClean atomic.Bool // scheduled leave — the drop is expected
	pumpDone  chan struct{}

	mu     sync.Mutex
	frames []wireFrame
	read   int // cursor of frames consumed by waitOp
}

// newEnetWireClient — connect + wait for EventConnect; the admit packet
// (token) is sent automatically in eventLoop. tokenB64 is the base64url from
// the HTTP response (join token or reconnect_token — both are raw tokens
// under base64).
func newEnetWireClient(t *testing.T, nick, tokenB64 string, port uint16) *enetWireClient {
	t.Helper()

	token, err := base64.RawURLEncoding.DecodeString(tokenB64)
	if err != nil {
		t.Fatalf("[%s] decode token: %v", nick, err)
	}

	loop, err := adapterenet.NewHostLoop(nil, 1, 2, nil)
	if err != nil {
		t.Fatalf("[%s] enet host: %v", nick, err)
	}
	c := &enetWireClient{
		t:        t,
		nick:     nick,
		token:    token,
		loop:     loop,
		pumpDone: make(chan struct{}),
	}
	// Insurance against a t.Fatal before stop(): Stop is idempotent.
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

	// Same as enetClient: serialize the handshake — parallel CONNECTs
	// occasionally lose datagrams and get rescued by libenet retries (~6s).
	waitFor(t, 15*time.Second, nick+" enet connect", func() bool {
		return c.connected.Load()
	})
	return c
}

// eventLoop — the client's pump: Service(1) through the HostLoop queue (the
// same confinement rules as enetClient) + event dispatch. EventReceive frames
// are appended to frames (OpCode + payload); exit — EventDisconnect, a loop
// error, or pumpStop.
func (c *enetWireClient) eventLoop() {
	defer close(c.pumpDone)
	admitted := false
	for {
		if c.pumpStop.Load() {
			return
		}
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
			c.mu.Lock()
			c.frames = append(c.frames, wireFrame{
				op:      domain.OpCode(data[0]),
				payload: append([]byte(nil), data[1:]...),
			})
			c.mu.Unlock()
		case libenet.EventDisconnect:
			c.connDead.Store(true)
			c.t.Logf("[%s] enet disconnected (data=%d, planned=%v)", c.nick, ev.GetData(), c.leftClean.Load())
			return
		}
	}
}

// disconnect — a graceful connection drop (like enetClient's leaveClean):
// the server gets the disconnect command → EventDisconnect → DisconnectPeer →
// the peer leaves the room into grace, the others get OpPeerDisconnected.
// Waits for the client's own EventDisconnect (bounded — the server may not
// get a libenet response out in time).
func (c *enetWireClient) disconnect() {
	c.leftClean.Store(true)
	select {
	case <-c.pumpDone:
		return
	default:
	}
	_ = c.loop.Do(func() error { c.enetPeer.Disconnect(0); return nil })
	select {
	case <-c.pumpDone:
	case <-time.After(5 * time.Second):
		c.t.Logf("[%s] disconnect: no EventDisconnect within 5s", c.nick)
	}
}

// stop — final teardown (idempotent after disconnect): a graceful disconnect
// if the pump is still alive, a bound on the pump's exit, then loop.Stop →
// host.Destroy. Destroy only after pumpDone — a Service on a destroyed host
// would be UB.
func (c *enetWireClient) stop() {
	c.leftClean.Store(true)
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
	c.loop.Stop()
}

// waitOp waits for a frame with the given OpCode on the frames feed (others
// are skipped: state patches/full are not asserts). The read cursor is shared
// per client: the scenario asserts run in frame arrival order. A dead
// connection before the frame → fail-fast; on a hit — the payload.
func (c *enetWireClient) waitOp(t *testing.T, op domain.OpCode, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		if c.read < len(c.frames) {
			f := c.frames[c.read]
			c.read++
			c.mu.Unlock()
			if f.op == op {
				return f.payload
			}
			c.t.Logf("[%s] skip frame op=0x%02x while waiting 0x%02x", c.nick, byte(f.op), byte(op))
			continue
		}
		c.mu.Unlock()
		if c.connDead.Load() {
			t.Fatalf("[%s] conn closed before opcode 0x%02x", c.nick, byte(op))
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("[%s] opcode 0x%02x not received within %v", c.nick, byte(op), timeout)
	return nil
}

// TestE2E_ENetReconnect — the basic P20 scenario over ENet: disconnect →
// OpPeerDisconnected at the host → resume with the reconnect token as the
// first packet → OpPeerReconnected + full state resync, nick from the slot
// (the token has none).
func TestE2E_ENetReconnect(t *testing.T) {
	t.Parallel()

	e := env.New(t, env.WithEnet())

	roomID, hostToken, hostPort := e.CreateRoomPort(t, "host")
	host := newEnetWireClient(t, "host", hostToken, hostPort)
	defer host.stop()

	joinToken, reconnectToken, joinerPort := e.IssueTicketReconnect(t, roomID, "joiner")
	joiner := newEnetWireClient(t, "joiner", joinToken, joinerPort)
	defer joiner.stop() // idempotent after the explicit stop below — insurance for a mid-test Fatal

	waitPeerCount(t, e, roomID, 2, 10*time.Second)
	joinerPeerID := peerIDOfNick(t, e, roomID, "joiner")

	// Disconnect: graceful disconnect → server-side EventDisconnect →
	// DisconnectPeer (slot into grace, peer leaves the room at once) → the
	// host gets OpPeerDisconnected{peerID u64 BE}.
	joiner.disconnect()
	joiner.stop()
	waitPeerCount(t, e, roomID, 1, 10*time.Second)

	payload := host.waitOp(t, domain.OpPeerDisconnected, 10*time.Second)
	if len(payload) != 8 || domain.PeerID(binary.BigEndian.Uint64(payload)) != joinerPeerID {
		t.Fatalf("OpPeerDisconnected payload = %x, want peerID %d", payload, joinerPeerID)
	}

	// Resume: reconnect_token as the first packet on the same host → admit
	// succeeds under the same peerID; the host gets OpPeerReconnected, the
	// returnee — a full state.
	resumed := newEnetWireClient(t, "joiner", reconnectToken, joinerPort)
	defer resumed.stop()
	waitPeerCount(t, e, roomID, 2, 10*time.Second)

	payload = host.waitOp(t, domain.OpPeerReconnected, 10*time.Second)
	if len(payload) != 8 || domain.PeerID(binary.BigEndian.Uint64(payload)) != joinerPeerID {
		t.Fatalf("OpPeerReconnected payload = %x, want peerID %d", payload, joinerPeerID)
	}

	if got := peerIDOfNick(t, e, roomID, "joiner"); got != joinerPeerID {
		t.Fatalf("resumed peerID = %d, want %d", got, joinerPeerID)
	}

	// Full state resync: the own peer is present, the nick — from the
	// reservation admitted slot (the reconnect token has no nick).
	fullPayload := resumed.waitOp(t, state.OpCodeFullState, 10*time.Second)
	full, err := codec.NewRoomStateCodec().Decode(fullPayload)
	if err != nil {
		t.Fatalf("decode full state: %v", err)
	}
	ps, ok := full.Peers[joinerPeerID]
	if !ok {
		t.Fatalf("full state has no peer %d (peers = %v)", joinerPeerID, full.Peers)
	}
	if ps.NickName != "joiner" {
		t.Fatalf("resumed nick = %q, want %q (from slot)", ps.NickName, "joiner")
	}
}

// TestE2E_ENetReconnectExpired — grace expiry (P24): a short ReconnectTTL,
// disconnect → wait past the TTL → resume → reject 0x74 OpReservationExpired.
// The orphan sweep is not running in env — the ReconnectRoom deadline gate
// fires before any revoke, the 0x74 branch is deterministic (the slot stays
// an admitted orphan).
func TestE2E_ENetReconnectExpired(t *testing.T) {
	t.Parallel()

	e := env.New(t, env.WithEnet(), env.WithReconnectTTL(300*time.Millisecond))

	roomID, hostToken, hostPort := e.CreateRoomPort(t, "host")
	host := newEnetWireClient(t, "host", hostToken, hostPort)
	defer host.stop()

	joinToken, reconnectToken, joinerPort := e.IssueTicketReconnect(t, roomID, "joiner")
	joiner := newEnetWireClient(t, "joiner", joinToken, joinerPort)
	defer joiner.stop()

	waitPeerCount(t, e, roomID, 2, 10*time.Second)
	joinerPeerID := peerIDOfNick(t, e, roomID, "joiner")

	joiner.disconnect()
	joiner.stop()
	waitPeerCount(t, e, roomID, 1, 10*time.Second)

	// Wait past the TTL: deadline = lastSeen (MarkDisconnected) + ReconnectTTL.
	time.Sleep(700 * time.Millisecond)

	resumed := newEnetWireClient(t, "joiner", reconnectToken, joinerPort)
	defer resumed.stop()

	resumed.waitOp(t, domain.OpReservationExpired, 10*time.Second)

	// The slot is untouched by the rejected attempt (SlotDetails is
	// read-only): still an admitted orphan — check the reservation state
	// directly.
	slotState, err := e.Reservation.State(context.Background(), roomID, joinerPeerID)
	if err != nil {
		t.Fatalf("reservation state: %v", err)
	}
	if slotState != domain.ReservationSlotAdmitted {
		t.Fatalf("reservation state = %v, want ReservationSlotAdmitted", slotState)
	}
}

// TestE2E_ENetReconnectWrongHost — strict binding (P18+P20): the reconnect
// token inherits the join ticket's claims.hostID. A resume packet sent to the
// port of the mux's other host is rejected with 0x77 OpWrongHost BEFORE the
// slot is consumed — the grace slot stays admitted, a resume on the assigned
// host afterwards succeeds.
func TestE2E_ENetReconnectWrongHost(t *testing.T) {
	t.Parallel()

	e := env.New(t, env.WithEnet(0, 0)) // two hosts: auto ports
	ports := e.EnetPorts()
	if len(ports) < 2 {
		t.Fatalf("multi-host env: need 2+ enet ports, got %v", ports)
	}

	roomID, _, _ := e.CreateRoomPort(t, "host")
	joinToken, reconnectToken, assignedPort := e.IssueTicketReconnect(t, roomID, "joiner")

	wrongPort := uint16(0)
	for _, p := range ports {
		if p != assignedPort {
			wrongPort = p
			break
		}
	}
	if wrongPort == 0 {
		t.Fatalf("no alternative enet port (assigned %d, ports %v)", assignedPort, ports)
	}

	// Join on the assigned host → disconnect → the slot is admitted in grace.
	joiner := newEnetWireClient(t, "joiner", joinToken, assignedPort)
	defer joiner.stop()
	waitPeerCount(t, e, roomID, 1, 10*time.Second)
	joinerPeerID := peerIDOfNick(t, e, roomID, "joiner")

	joiner.disconnect()
	joiner.stop()
	waitPeerCount(t, e, roomID, 0, 10*time.Second)

	// A resume packet on the foreign host → 0x77 OpWrongHost + graceful close.
	wrong := newEnetWireClient(t, "joiner", reconnectToken, wrongPort)
	defer wrong.stop()
	wrong.waitOp(t, domain.OpWrongHost, 10*time.Second)
	waitFor(t, 5*time.Second, "wrong-host client disconnected after reject", func() bool {
		return wrong.connDead.Load()
	})
	wrong.stop()

	// The slot is not consumed: admitted in grace — the correct-host resume succeeds.
	slotState, err := e.Reservation.State(context.Background(), roomID, joinerPeerID)
	if err != nil {
		t.Fatalf("reservation state: %v", err)
	}
	if slotState != domain.ReservationSlotAdmitted {
		t.Fatalf("reservation state = %v, want ReservationSlotAdmitted", slotState)
	}

	resumed := newEnetWireClient(t, "joiner", reconnectToken, assignedPort)
	defer resumed.stop()
	resumed.waitOp(t, state.OpCodeFullState, 10*time.Second)
	waitPeerCount(t, e, roomID, 1, 10*time.Second)
}
