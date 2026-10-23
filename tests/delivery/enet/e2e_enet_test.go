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

// enetTestClient - вспомогательный ENet-клиент на собственном HostLoop
// (adapterenet): все libenet-вызовы сериализует его loop-горутина, глобального
// mutex с серверным host'ом больше нет.
type enetTestClient struct {
	loop *adapterenet.HostLoop
	peer libenet.Peer
}

func newEnetTestClient(t *testing.T, port uint16) *enetTestClient {
	t.Helper()
	loop, err := adapterenet.NewHostLoop(nil, 1, 2)
	if err != nil {
		t.Fatalf("client host: %v", err)
	}
	// Stop идемпотентен: drain + host.Destroy + ожидание выхода loop-горутины.
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
		nil, // joinRoomUseCase — e2e покрывает только пути до JoinRoom
		nil, // leaveRoomUseCase
		adapterenet.NewEnetConnectionFactory(nil, discardLogger{}),
		cfg,
		discardLogger{},
	)
}

// P7-4: ошибка bind больше не проглатывается — ListenHandle.Done + Err().
func TestListenBindFailurePropagates(t *testing.T) {
	// Занимаем UDP-порт сами → NewHost (enet_host_create → bind) обязан упасть.
	blocker, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0})
	if err != nil {
		t.Fatalf("block udp port: %v", err)
	}
	defer func() { _ = blocker.Close() }()
	port := uint16(blocker.LocalAddr().(*net.UDPAddr).Port)

	cfg := adapterenet.DefaultEnetConfig()
	cfg.ListenPort = port
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

// P7-5: Stop отменяет loop и ждёт реального выхода; результат — context.Canceled.
func TestListenStopWaitsLoopExit(t *testing.T) {
	cfg := adapterenet.DefaultEnetConfig()
	cfg.ListenPort = freeUDPPort(t)
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

// P7-3: peer, не прошедший admit за AdmitTimeout, отключается сервером.
func TestAdmitTimeoutDisconnectsPeer(t *testing.T) {
	cfg := adapterenet.DefaultEnetConfig()
	cfg.ListenPort = freeUDPPort(t)
	cfg.AdmitTimeout = 200 * time.Millisecond
	cfg.ServiceTimeoutMs = 5
	handle := newTestHandler(cfg).Listen()
	defer handle.Stop()

	client := newEnetTestClient(t, cfg.ListenPort)
	client.waitEvent(t, libenet.EventConnect, 3*time.Second)

	// Ничего не шлём — сервер должен сам разорвать соединение.
	client.waitEvent(t, libenet.EventDisconnect, 5*time.Second)
}

// P7-1: join reject (пустой ticket → OpEmptyToken) реально доезжает до клиента
// до disconnect — graceful peer.Disconnect флешит queued reliable-пакет.
func TestJoinRejectFrameDelivered(t *testing.T) {
	cfg := adapterenet.DefaultEnetConfig()
	cfg.ListenPort = freeUDPPort(t)
	cfg.AdmitTimeout = 30 * time.Second // не должен сработать раньше reject-пути
	handle := newTestHandler(cfg).Listen()
	defer handle.Stop()

	client := newEnetTestClient(t, cfg.ListenPort)
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

	// После reject сервер инициирует graceful disconnect.
	client.waitEvent(t, libenet.EventDisconnect, 3*time.Second)
}
