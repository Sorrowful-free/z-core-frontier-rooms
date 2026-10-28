//go:build enet && cgo

package integration_test

import (
	"context"
	"encoding/base64"
	"sync"
	"testing"
	"time"

	adapterenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
	libenet "github.com/codecat/go-enet"
)

// shardClient - a lightweight ENet client for the host-sharding scenario (P18):
// connect, admit with a raw ticket, wait for the first frame / disconnect.
// Unlike enetClient (match_e2e_enet_test.go) it has no game loop - only the
// admit path is needed. Pump drives Service via commands through its own HostLoop.
type shardClient struct {
	loop *adapterenet.HostLoop
	peer libenet.Peer

	connected    chan struct{}
	firstFrame   chan byte
	disconnected chan struct{}
	connOnce     sync.Once
	disconnOnce  sync.Once
}

func newShardClient(t *testing.T, port uint16) *shardClient {
	t.Helper()

	loop, err := adapterenet.NewHostLoop(nil, 1, 2, nil)
	if err != nil {
		t.Fatalf("client host: %v", err)
	}
	c := &shardClient{
		loop:         loop,
		connected:    make(chan struct{}),
		firstFrame:   make(chan byte, 1),
		disconnected: make(chan struct{}),
	}
	t.Cleanup(c.stop)

	err = loop.Do(func() error {
		var err error
		c.peer, err = loop.Host().Connect(libenet.NewAddress("127.0.0.1", port), 2, 0)
		return err
	})
	if err != nil {
		t.Fatalf("enet connect port %d: %v", port, err)
	}

	go c.pump()
	select {
	case <-c.connected:
	case <-time.After(5 * time.Second):
		t.Fatalf("enet connect port %d timeout", port)
	}
	return c
}

func (c *shardClient) pump() {
	for {
		ev, err := adapterenet.Call(c.loop, func() libenet.Event { return c.loop.Host().Service(1) })
		if err != nil {
			return // loop stopped (cleanup)
		}
		switch ev.GetType() {
		case libenet.EventConnect:
			c.connOnce.Do(func() { close(c.connected) })
		case libenet.EventReceive:
			data, _ := adapterenet.Call(c.loop, func() []byte { return ev.GetPacket().GetData() })
			_ = c.loop.Do(func() error { ev.GetPacket().Destroy(); return nil })
			if len(data) > 0 {
				select {
				case c.firstFrame <- data[0]:
				default:
				}
			}
		case libenet.EventDisconnect:
			c.disconnOnce.Do(func() { close(c.disconnected) })
			return
		}
	}
}

// admit sends the base64url ticket (as returned by the HTTP response) as the first packet.
func (c *shardClient) admit(t *testing.T, token string) {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("decode ticket: %v", err)
	}
	c.admitRaw(t, raw)
}

func (c *shardClient) admitRaw(t *testing.T, token []byte) {
	t.Helper()
	if err := c.loop.Do(func() error {
		return c.peer.SendBytes(token, 0, libenet.PacketFlagReliable)
	}); err != nil {
		t.Fatalf("admit send: %v", err)
	}
}

func (c *shardClient) waitFirstFrame(t *testing.T, d time.Duration) byte {
	t.Helper()
	select {
	case op := <-c.firstFrame:
		return op
	case <-c.disconnected:
		t.Fatal("disconnected before first frame")
	case <-time.After(d):
		t.Fatalf("no frame within %v", d)
	}
	return 0
}

func (c *shardClient) waitDisconnect(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-c.disconnected:
	case <-time.After(d):
		t.Fatalf("no disconnect within %v", d)
	}
}

func (c *shardClient) stop() {
	if c.loop != nil {
		c.loop.Stop() // waits for the loop goroutine exit; pump exits on a Call error
	}
}

func otherEnetPort(t *testing.T, ports []uint16, assigned uint16) uint16 {
	t.Helper()
	for _, p := range ports {
		if p != assigned {
			return p
		}
	}
	t.Fatalf("no other enet port in %v (assigned %d)", ports, assigned)
	return 0
}

// TestE2E_ENetHostSharding - the P18 scenario: two hosts, server-side ticket
// balancing (least-loaded), strict-binding admit + a room gathering peers
// from different hosts (room is transport-agnostic).
func TestE2E_ENetHostSharding(t *testing.T) {
	if testing.Short() {
		t.Skip("enet cgo live test - skipped under -short")
	}
	t.Parallel()

	e := env.New(t, env.WithEnet(0, 0)) // 2 hosts on auto ports
	ports := e.EnetPorts()
	if len(ports) != 2 {
		t.Fatalf("enet ports = %v, want 2", ports)
	}

	// The host ticket is assigned to the first host; the joiner - to the other
	// (hosts are load-equal for the first ticket; once the host connects, the
	// second host is empty → least-loaded).
	roomID, hostToken, hostPort := e.CreateRoomPort(t, "host")
	if hostPort != ports[0] && hostPort != ports[1] {
		t.Fatalf("host ticket enet_port = %d, not in %v", hostPort, ports)
	}
	host := newShardClient(t, hostPort)
	host.admit(t, hostToken)
	e.WaitPeerCount(t, roomID, 1)

	joinerToken, joinerPort := e.IssueTicketPort(t, roomID, "p2")
	if joinerPort == hostPort {
		t.Fatalf("joiner ticket assigned to loaded host %d, want the empty one", joinerPort)
	}
	joiner := newShardClient(t, joinerPort)
	joiner.admit(t, joinerToken)
	// The room gathered peers from two hosts - sharding is per-peer, not per-room.
	e.WaitPeerCount(t, roomID, 2)

	// Strict binding: the ticket is bound to the assigned host; an admit on the
	// wrong one → reject OpWrongHost + disconnect.
	misToken, misPort := e.IssueTicketPort(t, roomID, "p3")
	wrong := newShardClient(t, otherEnetPort(t, ports, misPort))
	wrong.admit(t, misToken)
	if op := domain.OpCode(wrong.waitFirstFrame(t, 5*time.Second)); op != domain.OpWrongHost {
		t.Fatalf("reject frame op = %#x, want %#x (OpWrongHost)", byte(op), byte(domain.OpWrongHost))
	}
	wrong.waitDisconnect(t, 5*time.Second)

	// A reject on the wrong host does not consume the slot: the same ticket on
	// the right one → admit.
	right := newShardClient(t, misPort)
	right.admit(t, misToken)
	e.WaitPeerCount(t, roomID, 3)

	// Strict binding: a ticket without a binding (HostIDUnbound — issued
	// bypassing the usecase, as in a WS-only deploy) is rejected with
	// OpWrongHost on an ENet admit on any host; the reservation slot is not
	// consumed.
	ctx := context.Background()
	peerID, err := e.Allocator.AllocatePeerID(ctx)
	if err != nil {
		t.Fatalf("allocate peer id: %v", err)
	}
	if err := e.Reservation.Reserve(ctx, roomID, peerID, "unbound", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("reserve unbound slot: %v", err)
	}
	unboundToken, err := e.Admission.Issue(ctx, roomID, peerID, "unbound", domain.HostIDUnbound)
	if err != nil {
		t.Fatalf("issue unbound ticket: %v", err)
	}
	unbound := newShardClient(t, ports[0])
	unbound.admitRaw(t, unboundToken)
	if op := domain.OpCode(unbound.waitFirstFrame(t, 5*time.Second)); op != domain.OpWrongHost {
		t.Fatalf("reject frame op = %#x, want %#x (OpWrongHost)", byte(op), byte(domain.OpWrongHost))
	}
	unbound.waitDisconnect(t, 5*time.Second)
	state, err := e.Reservation.State(ctx, roomID, peerID)
	if err != nil || state != domain.ReservationSlotReserved {
		t.Fatalf("reservation state = %v (err %v), want reserved (слот не потрачен)", state, err)
	}
}
