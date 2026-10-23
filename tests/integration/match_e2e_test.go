package integration_test

import (
	"testing"
	"time"

	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
	fwebsocket "github.com/fasthttp/websocket"
)

// Сценарий e2e «живого матча» (план docs/plans/P11-e2e-match.md), ~60s
// реального времени:
//
//	host (master) + 3 slave по WS. Все шлют PatchInput ~20 Гц; клиент,
//	увидевший is_master на себе, дополнительно шлёт PatchEntities ~20 Гц
//	(entity id = peer id, Owner = свой peer id, позиция — u64 counter).
//
//	t=15s  host замолкает: read-pump останавливается, TCP-conn остаётся
//	       открытым → клиент не отвечает на ping → lastSeen стареет →
//	       idle sweep эвиктит за ~6s → миграция мастера на slave.
//	t=40s  текущий мастер делает clean leave → миграция №3.
//	t=60s  финал: ≥2 клиента живы, patches текут, peers в registry = 2.
//
// Общий клиентский слой и сценарный драйвер — match_e2e_client_test.go;
// ENet/mixed варианты — match_e2e_enet_test.go (tag enet && cgo).

var _ matchClient = (*wsClient)(nil)

// wsClient — минимальный клиент комнаты на WS: read-pump читает
// server→client кадры в общий handleFrame базы, send-loop — общий
// (sendLoop базы с writeFrame на websocket conn).
type wsClient struct {
	matchClientBase
	conn *fwebsocket.Conn
}

func newWSClient(t *testing.T, e *env.Env, nick, token string) *wsClient {
	t.Helper()

	c := &wsClient{matchClientBase: newMatchClientBase(t, nick)}
	c.conn = e.DialWS(t, "token="+token)
	go c.readPump()
	go c.sendLoop(c.writeFrame)
	go c.statsLoop()
	return c
}

// goSilent имитирует зависший клиент: перестаём читать (ping'и сервера больше
// не получают pong — fasthttp/websocket отвечает на ping только внутри
// ReadMessage) и перестаём слать. TCP-соединение остаётся открытым: сервер
// должен выкинуть peer по idle timeout, а не по TCP-разрыву.
func (c *wsClient) goSilent() {
	c.silent.Store(true)
	c.stopSend()
	// Будим заблокированный ReadMessage — TCP при этом не закрывается.
	_ = c.conn.SetReadDeadline(time.Now())
}

// leaveClean — плановый уход мастера №2: закрываем conn, серверный Receive
// падает → LeaveRoom → вторая миграция.
func (c *wsClient) leaveClean() {
	c.leftClean.Store(true)
	c.stopSend()
	_ = c.conn.Close()
}

// stop — финальный teardown клиента (идемпотентно для silent/leftClean).
func (c *wsClient) stop() {
	c.leftClean.Store(true)
	c.stopSend()
	_ = c.conn.Close()
	<-c.pumpDone
	<-c.sendDone
}

// --- read pump -------------------------------------------------------------

func (c *wsClient) readPump() {
	defer close(c.pumpDone)
	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			c.connDead.Store(true)
			switch {
			case c.silent.Load():
				c.t.Logf("[%s] read stopped — silent mode, TCP stays open", c.nick)
			case c.leftClean.Load():
				c.t.Logf("[%s] connection closed (planned)", c.nick)
			default:
				c.t.Logf("[%s] disconnected: %v", c.nick, err)
			}
			return
		}
		if len(msg) == 0 {
			continue
		}
		c.handleFrame(domain.OpCode(msg[0]), msg[1:])
	}
}

func (c *wsClient) writeFrame(op domain.OpCode, payload []byte) error {
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.conn.WriteMessage(fwebsocket.BinaryMessage, append([]byte{byte(op)}, payload...))
}

// TestE2E_LiveMatch_HostMigration — см. шапку файла и docs/plans/P11-e2e-match.md.
func TestE2E_LiveMatch_HostMigration(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e live match ~60s real time — skipped under -short")
	}
	t.Parallel()

	e := env.New(t,
		env.WithTransport(adaptertransport.TransportConfig{
			RoomIncomingQueueSize: adaptertransport.DefaultRoomIncomingQueueSize,
			PeerOutboundQueueSize: adaptertransport.DefaultPeerOutboundQueueSize,
			EnetIncomingQueueSize: adaptertransport.DefaultEnetIncomingQueueSize,
			MaxIncomingFrameBytes: adaptertransport.DefaultMaxIncomingFrameBytes,
			WSWriteDeadline:       adaptertransport.DefaultWSWriteDeadline,
			// PeerIdleTimeout > wsPingInterval (2s): живые клиенты держатся
			// за pong → lastSeen; замолчавший отваливается за timeout+sweep.
			PeerIdleTimeout:       5 * time.Second,
			PeerIdleSweepInterval: time.Second,
		}),
		env.WithIdleSweep(time.Second),
	)

	roomID, hostToken := e.CreateRoom(t, "host")
	host := newWSClient(t, e, "host", hostToken)
	s1 := newWSClient(t, e, "s1", e.IssueTicket(t, roomID, "s1"))
	s2 := newWSClient(t, e, "s2", e.IssueTicket(t, roomID, "s2"))
	s3 := newWSClient(t, e, "s3", e.IssueTicket(t, roomID, "s3"))
	clients := []*wsClient{host, s1, s2, s3}
	defer func() {
		for _, c := range clients {
			c.stop()
		}
	}()

	runLiveMatch(t, e, roomID, host, []matchClient{s1, s2, s3}, liveMatchTiming{
		hostSilentAt:  15 * time.Second,
		evictBound:    15 * time.Second, // 5s idle timeout + 1s sweep + миграция
		secondLeaveAt: 40 * time.Second,
		finishAt:      60 * time.Second,
	})
}
