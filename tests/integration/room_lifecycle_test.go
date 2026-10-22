package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
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

// TestIntegration_TicketReplay_WS — D3: тикет одноразовый. Повторный join
// тем же токеном (слот уже admitted) → JoinReject-кадр 0x75
// (OpReservationAlreadyAdmitted). Реконнект идёт через новый тикет.
func TestIntegration_TicketReplay_WS(t *testing.T) {
	t.Parallel()

	e := env.New(t)

	roomID, hostToken := e.CreateRoom(t, "host")
	hostConn := e.DialWS(t, "token="+hostToken)
	defer hostConn.Close()
	e.WaitPeerCount(t, roomID, 1)

	replay := e.DialWS(t, "token="+hostToken)
	defer replay.Close()

	if err := replay.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, msg, err := replay.ReadMessage()
	if err != nil {
		t.Fatalf("read reject frame: %v", err)
	}
	if len(msg) < 1 || domain.OpCode(msg[0]) != domain.OpReservationAlreadyAdmitted {
		t.Fatalf("reject frame = %v, want opcode 0x75 (OpReservationAlreadyAdmitted)", msg)
	}
}
