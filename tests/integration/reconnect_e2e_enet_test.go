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

// E2E reconnect по реальному ENet-транспорту (план docs/plans/P22-enet-reconnect-e2e.md,
// поверх семантики P20/P24 и WS-покрытия reconnect_test.go):
//
//	TestE2E_ENetReconnect          — разрыв → 0x80 OpPeerDisconnected у host'а →
//	                                resume reconnect_token (v4) первым пакетом →
//	                                0x81 OpPeerReconnected у host'а + full state
//	                                resync вернувшемуся, nick из слота.
//	TestE2E_ENetReconnectExpired   — короткий ReconnectTTL: resume past grace →
//	                                0x74 OpReservationExpired (deadline-гейт P24;
//	                                orphan sweep в env не поднят → ветка
//	                                детерминирована, 0x73 недостижим).
//	TestE2E_ENetReconnectWrongHost — multi-host env: v4-токен наследует
//	                                claims.hostID join-тикета; resume-пакет на
//	                                чужом host'е → 0x77 OpWrongHost, слот не
//	                                тратится (correct-host resume после проходит).
//
// Клиент — облегчённый enetWireClient ниже: plumbing тот же, что у enetClient
// из match_e2e_enet_test.go (HostLoop, Call/Do, admit первым пакетом), но без
// match-машинерии (input/entities-стримы не нужны) и с записью сырых
// wire-кадров — общий клиент ленту 0x7x/0x8x opcodes не наблюдает.

// wireFrame — принятый серверный кадр (OpCode + payload) в порядке прихода.
type wireFrame struct {
	op      domain.OpCode
	payload []byte
}

// enetWireClient — in-test ENet-клиент для reconnect-сценариев: connect →
// admit первым пакетом (произвольный raw-token: join v3 или reconnect v4) →
// события Service в ленту frames → graceful disconnect → stop.
type enetWireClient struct {
	t    *testing.T
	nick string

	loop     *adapterenet.HostLoop
	enetPeer libenet.Peer
	token    []byte // admit: первый пакет после EventConnect — сырой (decoded) token

	connected atomic.Bool // EventConnect получен — handshake завершён
	connDead  atomic.Bool // pump увидел ошибку/разрыв соединения
	pumpStop  atomic.Bool // stop(): pump выходит без ожидания EventDisconnect
	leftClean atomic.Bool // плановый уход — разрыв ожидаем
	pumpDone  chan struct{}

	mu     sync.Mutex
	frames []wireFrame
	read   int // курсор потреблённых кадров waitOp
}

// newEnetWireClient — connect + ожидание EventConnect; admit-пакет (token)
// уходит автоматически в eventLoop. tokenB64 — base64url из HTTP-ответа
// (join token или reconnect_token — оба сырые токены под base64).
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
	// Страховка на случай t.Fatal до stop(): Stop идемпотентен.
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

	// Как у enetClient: сериализуем handshake — параллельные CONNECT иногда
	// теряют дейтаграммы и достаются libenet-ретраями (~6s).
	waitFor(t, 15*time.Second, nick+" enet connect", func() bool {
		return c.connected.Load()
	})
	return c
}

// eventLoop — pump клиента: Service(1) через очередь HostLoop (те же
// правила confinement, что у enetClient) + dispatch событий. Кадры
// EventReceive складываются в frames (OpCode + payload); выход —
// EventDisconnect, ошибка loop'а либо pumpStop.
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

// disconnect — graceful разрыв соединения (как leaveClean у enetClient):
// сервер получает disconnect-команду → EventDisconnect → DisconnectPeer →
// peer из комнаты в grace, остальным OpPeerDisconnected. Ждёт собственный
// EventDisconnect клиента (bound — сервер libenet-ответа может не успеть).
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

// stop — финальный teardown (идемпотентен после disconnect): graceful
// disconnect, если pump ещё жив, bound на выход pump'а, затем loop.Stop →
// host.Destroy. Destroy только после pumpDone — Service на разрушенном
// host'е был бы UB.
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

// waitOp ждёт кадр с заданным OpCode по ленте frames (прочие пропускаются:
// state-патчи/full — не ассерты). Курсор read — общий на клиента: ассерты
// сценария идут в порядке прихода кадров. Мёртвая соединение до кадра →
// fail-fast; найденного — payload.
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

// TestE2E_ENetReconnect — базовый сценарий P20 поверх ENet: разрыв →
// OpPeerDisconnected у host'а → resume v4-токеном первым пакетом →
// OpPeerReconnected + full state resync, nick из слота (в v4 его нет).
func TestE2E_ENetReconnect(t *testing.T) {
	t.Parallel()

	e := env.New(t, env.WithEnet())

	roomID, hostToken, hostPort := e.CreateRoomPort(t, "host")
	host := newEnetWireClient(t, "host", hostToken, hostPort)
	defer host.stop()

	joinToken, reconnectToken, joinerPort := e.IssueTicketReconnect(t, roomID, "joiner")
	joiner := newEnetWireClient(t, "joiner", joinToken, joinerPort)
	defer joiner.stop() // идемпотентен после явного stop ниже — страховка на mid-test Fatal

	waitPeerCount(t, e, roomID, 2, 10*time.Second)
	joinerPeerID := peerIDOfNick(t, e, roomID, "joiner")

	// Разрыв: graceful disconnect → серверный EventDisconnect →
	// DisconnectPeer (слот в grace, peer из комнаты сразу) → host'у
	// OpPeerDisconnected{peerID u64 BE}.
	joiner.disconnect()
	joiner.stop()
	waitPeerCount(t, e, roomID, 1, 10*time.Second)

	payload := host.waitOp(t, domain.OpPeerDisconnected, 10*time.Second)
	if len(payload) != 8 || domain.PeerID(binary.BigEndian.Uint64(payload)) != joinerPeerID {
		t.Fatalf("OpPeerDisconnected payload = %x, want peerID %d", payload, joinerPeerID)
	}

	// Resume: reconnect_token (v4) первым пакетом на том же host'е → admit
	// успешен под тем же peerID; host'у OpPeerReconnected, вернувшемуся —
	// full state.
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

	// Full state resync: свой peer присутствует, nick — из admitted-слота
	// reservation (в токене v4 nick'а нет).
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

// TestE2E_ENetReconnectExpired — grace-expiry (P24): короткий ReconnectTTL,
// разрыв → ожидание past TTL → resume → reject 0x74 OpReservationExpired.
// Orphan sweep в env не поднят — дедлайн-гейт ReconnectRoom срабатывает до
// любого revoke, ветка 0x74 детерминирована (слот остаётся admitted-сиротой).
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

	// Ждём past TTL: deadline = lastSeen (MarkDisconnected) + ReconnectTTL.
	time.Sleep(700 * time.Millisecond)

	resumed := newEnetWireClient(t, "joiner", reconnectToken, joinerPort)
	defer resumed.stop()

	resumed.waitOp(t, domain.OpReservationExpired, 10*time.Second)

	// Слот не тронут отклонённой попыткой (SlotDetails read-only): всё ещё
	// admitted-сирота — проверяем состояние reservation напрямую.
	slotState, err := e.Reservation.State(context.Background(), roomID, joinerPeerID)
	if err != nil {
		t.Fatalf("reservation state: %v", err)
	}
	if slotState != domain.ReservationSlotAdmitted {
		t.Fatalf("reservation state = %v, want ReservationSlotAdmitted", slotState)
	}
}

// TestE2E_ENetReconnectWrongHost — strict binding v4 (P18+P20): reconnect
// -токен наследует claims.hostID join-тикета. Resume-пакет, отправленный на
// порт другого host'а mux'а, отклоняется 0x77 OpWrongHost ДО траты слота —
// grace-слот остаётся admitted, resume на назначенном host'е после проходит.
func TestE2E_ENetReconnectWrongHost(t *testing.T) {
	t.Parallel()

	e := env.New(t, env.WithEnet(0, 0)) // два host'а: порты auto
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

	// Join на назначенном host'е → разрыв → слот admitted в grace.
	joiner := newEnetWireClient(t, "joiner", joinToken, assignedPort)
	defer joiner.stop()
	waitPeerCount(t, e, roomID, 1, 10*time.Second)
	joinerPeerID := peerIDOfNick(t, e, roomID, "joiner")

	joiner.disconnect()
	joiner.stop()
	waitPeerCount(t, e, roomID, 0, 10*time.Second)

	// Resume-пакет на чужом host'е → 0x77 OpWrongHost + graceful close.
	wrong := newEnetWireClient(t, "joiner", reconnectToken, wrongPort)
	defer wrong.stop()
	wrong.waitOp(t, domain.OpWrongHost, 10*time.Second)
	waitFor(t, 5*time.Second, "wrong-host client disconnected after reject", func() bool {
		return wrong.connDead.Load()
	})
	wrong.stop()

	// Слот не тратится: admitted в grace — correct-host resume проходит.
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
