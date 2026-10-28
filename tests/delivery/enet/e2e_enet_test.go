//go:build enet && cgo

package enet_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	adapterenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	deliveryenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

func freeUDPPort(t *testing.T) uint16 {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("reserve udp port: %v", err)
	}
	defer func() { _ = conn.Close() }()
	return uint16(conn.LocalAddr().(*net.UDPAddr).Port)
}

// enetTestClient — a helper ENet client on its own HostLoop (adapterenet):
// all libenet calls are serialized by its loop goroutine, there is no
// global mutex shared with the server host anymore.
type enetTestClient struct {
	loop *adapterenet.HostLoop
	peer libenet.Peer
}

func newEnetTestClient(t *testing.T, port uint16) *enetTestClient {
	t.Helper()
	loop, err := adapterenet.NewHostLoop(nil, 1, 2, nil)
	if err != nil {
		t.Fatalf("client host: %v", err)
	}
	// Stop is idempotent: drain + host.Destroy + wait for the loop goroutine to exit.
	t.Cleanup(loop.Stop)

	var peer libenet.Peer
	err = loop.Do(func() error {
		var err error
		peer, err = loop.Host().Connect(libenet.NewAddress("127.0.0.1", port), 2, 0)
		return err
	})
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	return &enetTestClient{loop: loop, peer: peer}
}

func (c *enetTestClient) waitEvent(t *testing.T, want libenet.EventType, timeout time.Duration) libenet.Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ev, err := adapterenet.Call(c.loop, func() libenet.Event { return c.loop.Host().Service(10) })
		if err != nil {
			t.Fatalf("client service: %v", err)
		}
		switch ev.GetType() {
		case libenet.EventNone:
			continue
		case want:
			return ev
		default:
			t.Fatalf("unexpected event %v, want %v", ev.GetType(), want)
		}
	}
	t.Fatalf("event %v not received within %v", want, timeout)
	return nil
}

func newTestHandler(cfg adapterenet.EnetConfig) *deliveryenet.RoomsHandler {
	return deliveryenet.NewRoomsHandler(
		context.Background(),
		nil, // admitPeerUseCase — e2e only covers the paths up to admit
		nil, // disconnectPeerUseCase
		adapterenet.NewEnetConnectionFactory(nil, discardLogger{}),
		cfg,
		discardLogger{},
		nil,
	)
}

// P7-4: a bind error is no longer swallowed — ListenHandle.Done + Err().
func TestListenBindFailurePropagates(t *testing.T) {
	// Occupy the UDP port ourselves → NewHost (enet_host_create → bind) must fail.
	blocker, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0})
	if err != nil {
		t.Fatalf("block udp port: %v", err)
	}
	defer func() { _ = blocker.Close() }()
	port := uint16(blocker.LocalAddr().(*net.UDPAddr).Port)

	cfg := adapterenet.DefaultEnetConfig()
	cfg.ListenPorts = []uint16{port}
	handle := newTestHandler(cfg).Listen()

	select {
	case <-handle.Done():
		if handle.Err() == nil {
			t.Fatal("Listen exited cleanly on occupied port, want bind error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Listen did not terminate on bind failure")
	}
}

// P7-5: Stop cancels the loop and waits for the real exit; the result is context.Canceled.
func TestListenStopWaitsLoopExit(t *testing.T) {
	cfg := adapterenet.DefaultEnetConfig()
	cfg.ListenPorts = []uint16{freeUDPPort(t)}
	handle := newTestHandler(cfg).Listen()

	stopped := make(chan struct{})
	go func() {
		handle.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not wait for host loop exit")
	}
	if err := handle.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Err() = %v, want context.Canceled", err)
	}
}

// P7-3: a peer that did not pass admit within AdmitTimeout is disconnected by the server.
func TestAdmitTimeoutDisconnectsPeer(t *testing.T) {
	cfg := adapterenet.DefaultEnetConfig()
	cfg.ListenPorts = []uint16{freeUDPPort(t)}
	cfg.AdmitTimeout = 200 * time.Millisecond
	cfg.ServiceTimeoutMs = 5
	handle := newTestHandler(cfg).Listen()
	defer handle.Stop()

	client := newEnetTestClient(t, cfg.ListenPorts[0])
	client.waitEvent(t, libenet.EventConnect, 3*time.Second)

	// We send nothing — the server must tear down the connection itself.
	client.waitEvent(t, libenet.EventDisconnect, 5*time.Second)
}

// P7-1: a join reject (empty ticket → OpEmptyToken) really reaches the
// client before the disconnect — graceful peer.Disconnect flushes the
// queued reliable packet.
func TestJoinRejectFrameDelivered(t *testing.T) {
	cfg := adapterenet.DefaultEnetConfig()
	cfg.ListenPorts = []uint16{freeUDPPort(t)}
	cfg.AdmitTimeout = 30 * time.Second // must not fire before the reject path
	handle := newTestHandler(cfg).Listen()
	defer handle.Stop()

	client := newEnetTestClient(t, cfg.ListenPorts[0])
	client.waitEvent(t, libenet.EventConnect, 3*time.Second)

	err := client.loop.Do(func() error {
		return client.peer.SendBytes([]byte{}, 0, libenet.PacketFlagReliable)
	})
	if err != nil {
		t.Fatalf("send empty admit packet: %v", err)
	}

	ev := client.waitEvent(t, libenet.EventReceive, 3*time.Second)
	data, err := adapterenet.Call(client.loop, func() []byte { return ev.GetPacket().GetData() })
	if err != nil {
		t.Fatalf("packet get data: %v", err)
	}
	_ = client.loop.Do(func() error { ev.GetPacket().Destroy(); return nil })
	if len(data) != 1 || domain.OpCode(data[0]) != domain.OpEmptyToken {
		t.Fatalf("reject frame = %v, want single byte %#x", data, byte(domain.OpEmptyToken))
	}

	// After the reject the server initiates a graceful disconnect.
	client.waitEvent(t, libenet.EventDisconnect, 3*time.Second)
}
