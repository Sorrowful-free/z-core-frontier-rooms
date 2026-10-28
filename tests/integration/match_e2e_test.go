package integration_test

import (
	"testing"
	"time"

	adaptertransport "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
	fwebsocket "github.com/fasthttp/websocket"
)

// E2E "live match" scenario (plan docs/plans/P11-e2e-match.md), ~60s of
// real time:
//
//	host (master) + 3 slaves over WS. Everyone sends PatchInput at ~20Hz;
//	the client that sees is_master on itself additionally sends
//	PatchEntities at ~20Hz (entity id = peer id, Owner = own peer id,
//	position — a u64 counter).
//
//	t=15s  the host goes silent: the read pump stops, the TCP conn stays
//	       open → the client no longer answers pings → lastSeen goes stale →
//	       the idle sweep evicts it within ~6s → master migrates to a slave.
//	t=40s  the current master does a clean leave → migration #3.
//	t=60s  final: ≥2 clients alive, patches flowing, peers in registry = 2.
//
// The shared client layer and scenario driver — match_e2e_client_test.go;
// the ENet/mixed variants — match_e2e_enet_test.go (tag enet && cgo).

var _ matchClient = (*wsClient)(nil)

// wsClient — a minimal room client over WS: the read pump reads
// server→client frames into the shared base handleFrame, the send loop is
// shared too (the base sendLoop with writeFrame on the websocket conn).
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

// goSilent simulates a hung client: we stop reading (the server's pings no
// longer get pongs — fasthttp/websocket answers a ping only inside
// ReadMessage) and stop sending. The TCP connection stays open: the server
// must evict the peer on idle timeout, not on a TCP drop.
func (c *wsClient) goSilent() {
	c.silent.Store(true)
	c.stopSend()
	// Wake the blocked ReadMessage — TCP stays open.
	_ = c.conn.SetReadDeadline(time.Now())
}

// leaveClean — the scheduled departure of master #2: we close the conn, the
// server-side Receive fails → LeaveRoom → the second migration.
func (c *wsClient) leaveClean() {
	c.leftClean.Store(true)
	c.stopSend()
	_ = c.conn.Close()
}

// stop — the final client teardown (idempotent for silent/leftClean).
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

// TestE2E_LiveMatch_HostMigration — see the file header and docs/plans/P11-e2e-match.md.
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
			// PeerIdleTimeout > wsPingInterval (2s): live clients hold on via
			// pong → lastSeen; the silent one drops after timeout+sweep.
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
		evictBound:    15 * time.Second, // 5s idle timeout + 1s sweep + migration
		secondLeaveAt: 40 * time.Second,
		finishAt:      60 * time.Second,
	})
}
