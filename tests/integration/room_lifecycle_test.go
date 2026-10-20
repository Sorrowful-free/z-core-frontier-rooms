package integration_test

import (
	"context"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
)

// TestIntegration_RoomLifecycle_WS — первый сквозной сценарий ADR-0002:
// create (host) → issue (joiner) → оба join по WS → 2 peers →
// joiner disconnect → 1 peer (host). Disconnect→leave проверяется poll'ом
// по registry, а не по времени: conn.Close → серверный Receive ошибка →
// LeaveRoomUseCase убирает peer'а из комнаты.
func TestIntegration_RoomLifecycle_WS(t *testing.T) {
	t.Parallel()

	e := env.New(t)

	// host: create возвращает ticket, join по WS.
	roomID, hostToken := e.CreateRoom(t, "host")
	hostConn := e.DialWS(t, "token="+hostToken)
	defer hostConn.Close()
	e.WaitPeerCount(t, roomID, 1)

	// joiner: отдельный ticket через POST /rooms/:id/tickets.
	joinerToken := e.IssueTicket(t, roomID, "joiner")
	joinerConn := e.DialWS(t, "token="+joinerToken)
	defer joinerConn.Close()
	e.WaitPeerCount(t, roomID, 2)
	e.WaitPeerNick(t, roomID, "host", true)
	e.WaitPeerNick(t, roomID, "joiner", true)

	// disconnect joiner'а → LeaveRoom → остаётся только host.
	if err := joinerConn.Close(); err != nil {
		t.Fatalf("joiner conn close: %v", err)
	}
	e.WaitPeerCount(t, roomID, 1)

	room, err := e.Registry.GetRoom(context.Background(), roomID)
	if err != nil {
		t.Fatalf("get room %d: %v", roomID, err)
	}
	peers := room.GetPeers()
	if len(peers) != 1 {
		t.Fatalf("peers len = %d, want 1", len(peers))
	}
	if got := peers[0].GetNickName(); got != "host" {
		t.Fatalf("remaining peer nick = %q, want %q", got, "host")
	}
}
