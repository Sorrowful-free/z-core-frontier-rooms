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

// shardClient - лёгкий ENet-клиент для сценария host-sharding (P18): connect,
// admit raw ticket'ом, ожидание первого кадра / disconnect. В отличие от
// enetClient (match_e2e_enet_test.go) без игрового контура - нужен только
// admit-путь. Pump гоняет Service командами через собственный HostLoop.
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

	loop, err := adapterenet.NewHostLoop(nil, 1, 2)
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
			return // loop остановлен (cleanup)
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

// admit шлёт base64url-тикет (как из HTTP-ответа) первым пакетом.
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
		c.loop.Stop() // ждёт выхода loop-горутины; pump выходит по ошибке Call
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

// TestE2E_ENetHostSharding - сценарий P18: два host'а, серверная балансировка
// ticket'ов (least-loaded), strict binding admit + комната, собирающая peer'ов
// с разных host'ов (room - транспорт-агностик).
func TestE2E_ENetHostSharding(t *testing.T) {
	if testing.Short() {
		t.Skip("enet cgo live test - skipped under -short")
	}
	t.Parallel()

	e := env.New(t, env.WithEnet(0, 0)) // 2 host'а на auto-портах
	ports := e.EnetPorts()
	if len(ports) != 2 {
		t.Fatalf("enet ports = %v, want 2", ports)
	}

	// Host ticket назначается на первый host; joiner - на другой (host'ы
	// равны по load у первого тикета, после подключения host'а второй host
	// пуст → least-loaded).
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
	// Комната собрала peer'ов с двух host'ов - sharding по peer, не по комнате.
	e.WaitPeerCount(t, roomID, 2)

	// Strict binding: тикет привязан к назначенному host'у; admit на чужом →
	// reject OpWrongHost + disconnect.
	misToken, misPort := e.IssueTicketPort(t, roomID, "p3")
	wrong := newShardClient(t, otherEnetPort(t, ports, misPort))
	wrong.admit(t, misToken)
	if op := domain.OpCode(wrong.waitFirstFrame(t, 5*time.Second)); op != domain.OpWrongHost {
		t.Fatalf("reject frame op = %#x, want %#x (OpWrongHost)", byte(op), byte(domain.OpWrongHost))
	}
	wrong.waitDisconnect(t, 5*time.Second)

	// Reject на чужом host'е не тратит слот: тот же тикет на правильном → admit.
	right := newShardClient(t, misPort)
	right.admit(t, misToken)
	e.WaitPeerCount(t, roomID, 3)

	// Backward compat: ticket без hostID (HostIDAny) admit'ится на любом host'е.
	ctx := context.Background()
	peerID, err := e.Allocator.AllocatePeerID(ctx)
	if err != nil {
		t.Fatalf("allocate peer id: %v", err)
	}
	if err := e.Reservation.Reserve(ctx, roomID, peerID, "legacy", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("reserve legacy slot: %v", err)
	}
	legacyToken, err := e.Admission.Issue(ctx, roomID, peerID, "legacy", domain.HostIDAny)
	if err != nil {
		t.Fatalf("issue legacy ticket: %v", err)
	}
	legacy := newShardClient(t, otherEnetPort(t, ports, misPort))
	legacy.admitRaw(t, legacyToken)
	e.WaitPeerCount(t, roomID, 4)
}
