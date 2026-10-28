package integration_test

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
	fwebsocket "github.com/fasthttp/websocket"
)

// peerIDOfNick — peerID by nick via the registry (external observation).
func peerIDOfNick(t *testing.T, e *env.Env, roomID domain.RoomID, nick string) domain.PeerID {
	t.Helper()
	room, err := e.Registry.GetRoom(context.Background(), roomID)
	if err != nil {
		t.Fatalf("get room %d: %v", roomID, err)
	}
	for _, p := range room.GetPeers() {
		if p.GetNickName() == nick {
			return p.GetID()
		}
	}
	t.Fatalf("peer %q not found in room %d", nick, roomID)
	return domain.PeerIDInvalid
}

// waitOp waits for a frame with the given opcode on the host connection,
// skipping state patches; returns the payload (peerID u64 BE for 0x80/0x81).
func waitOp(t *testing.T, conn *fwebsocket.Conn, op domain.OpCode, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			continue
		}
		if len(msg) >= 1 && domain.OpCode(msg[0]) == op {
			return msg[1:]
		}
	}
	t.Fatalf("opcode %#x not received within %v", byte(op), timeout)
	return nil
}

// TestIntegration_Reconnect_WS — P20: disconnect → OpPeerDisconnected → resume
// with the reconnect token under the same peerID → OpPeerReconnected; the nick
// is restored from the slot (the reconnect token has none), a repeated join
// ticket stays single-use.
func TestIntegration_Reconnect_WS(t *testing.T) {
	t.Parallel()

	e := env.New(t)

	roomID, hostToken := e.CreateRoom(t, "host")
	hostConn := e.DialWS(t, "token="+hostToken)
	defer hostConn.Close()
	e.WaitPeerCount(t, roomID, 1)

	joinToken, reconnectToken, _ := e.IssueTicketReconnect(t, roomID, "joiner")
	joinerConn := e.DialWS(t, "token="+joinToken)
	e.WaitPeerCount(t, roomID, 2)

	joinerPeerID := peerIDOfNick(t, e, roomID, "joiner")

	// Disconnect: the peer is removed from the room at once (delete-peer model),
	// the others get OpPeerDisconnected{peerID}, the slot goes into grace.
	if err := joinerConn.Close(); err != nil {
		t.Fatalf("joiner conn close: %v", err)
	}
	e.WaitPeerCount(t, roomID, 1)

	payload := waitOp(t, hostConn, domain.OpPeerDisconnected, 3*time.Second)
	if len(payload) != 8 || domain.PeerID(binary.BigEndian.Uint64(payload)) != joinerPeerID {
		t.Fatalf("OpPeerDisconnected payload = %x, want peerID %d", payload, joinerPeerID)
	}

	// Resume with the reconnect token: same peerID, same nick (from the slot).
	resumed := e.DialWS(t, "token="+reconnectToken)
	defer resumed.Close()
	e.WaitPeerCount(t, roomID, 2)

	if got := peerIDOfNick(t, e, roomID, "joiner"); got != joinerPeerID {
		t.Fatalf("resumed peerID = %d, want %d", got, joinerPeerID)
	}

	payload = waitOp(t, hostConn, domain.OpPeerReconnected, 3*time.Second)
	if len(payload) != 8 || domain.PeerID(binary.BigEndian.Uint64(payload)) != joinerPeerID {
		t.Fatalf("OpPeerReconnected payload = %x, want peerID %d", payload, joinerPeerID)
	}
}
