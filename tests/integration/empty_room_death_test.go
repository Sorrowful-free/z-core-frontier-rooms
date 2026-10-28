package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/env"
)

// listRoomIDs returns the set of roomIDs from GET /rooms.
func listRoomIDs(t *testing.T, e *env.Env) map[int64]bool {
	t.Helper()
	resp, body := e.Do(t, http.MethodGet, "/rooms", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /rooms status = %d, body = %s", resp.StatusCode, body)
	}
	var list struct {
		Rooms []struct {
			ID int64 `json:"id"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("unmarshal GET /rooms: %v", err)
	}
	ids := make(map[int64]bool, len(list.Rooms))
	for _, r := range list.Rooms {
		ids[r.ID] = true
	}
	return ids
}

// P31 emptied-death: disconnecting the last peer leaves the room alive
// in grace (the admitted slot keeps it — resume is possible); when the orphan
// sweep revokes the last grace slot, the room dies — it disappears from the
// registry and from GET /rooms.
func TestIntegration_EmptyRoom_LastGraceSlotRevoked_KillsRoom(t *testing.T) {
	t.Parallel()

	e := env.New(t)
	ctx := context.Background()

	roomID, hostToken := e.CreateRoom(t, "host")
	hostConn := e.DialWS(t, "token="+hostToken)
	e.WaitPeerCount(t, roomID, 1)

	if err := hostConn.Close(); err != nil {
		t.Fatalf("host conn close: %v", err)
	}
	e.WaitPeerCount(t, roomID, 0)

	// Grace still holds the room: the slot is admitted, no peers — not "empty".
	if _, err := e.Registry.GetRoom(ctx, roomID); err != nil {
		t.Fatalf("room must survive disconnect within grace: %v", err)
	}
	slots, err := e.Reservation.ListAdmittedPeers(ctx, roomID)
	if err != nil || len(slots) != 1 {
		t.Fatalf("admitted slots after disconnect = %v, %v; want 1 grace slot", slots, err)
	}

	// TTL=0: graceExpired is true at once — the sweep revokes the last slot.
	sweep := useroom.NewSweepOrphanAdmittedUseCase(e.Registry, e.Reservation, 0, stdlib.New("test"), nil)
	revoked, err := sweep.RunOnce(ctx)
	if err != nil {
		t.Fatalf("orphan sweep RunOnce: %v", err)
	}
	if revoked < 1 {
		t.Fatalf("revoked = %d, want >= 1", revoked)
	}

	if _, err := e.Registry.GetRoom(ctx, roomID); !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("GetRoom err = %v, want ErrRoomNotFound", err)
	}
	if _, err := e.Reservation.ListAdmittedPeers(ctx, roomID); !errors.Is(err, domain.ErrReservationNotFound) {
		t.Fatalf("ListAdmittedPeers err = %v, want ErrReservationNotFound", err)
	}
	if listRoomIDs(t, e)[int64(roomID)] {
		t.Fatal("room still present in GET /rooms after empty death")
	}
}

// P31: disconnecting the last peer within the TTL does NOT kill the room —
// resume via the reconnect token works (regression: killing on bare peers==0
// would break P20 resume — there would be nothing to return to).
func TestIntegration_EmptyRoom_LastPeerResumeWithinTTL(t *testing.T) {
	t.Parallel()

	e := env.New(t)
	ctx := context.Background()

	roomID, joinToken, reconnectToken := e.CreateRoomReconnect(t, "host")
	hostConn := e.DialWS(t, "token="+joinToken)
	e.WaitPeerCount(t, roomID, 1)
	hostPeerID := peerIDOfNick(t, e, roomID, "host")

	if err := hostConn.Close(); err != nil {
		t.Fatalf("host conn close: %v", err)
	}
	e.WaitPeerCount(t, roomID, 0)

	// The room is alive: the admitted grace slot holds it.
	if _, err := e.Registry.GetRoom(ctx, roomID); err != nil {
		t.Fatalf("room must survive last-peer disconnect within TTL: %v", err)
	}

	// Resume under the same peerID — the room accepts the reconnect.
	resumed := e.DialWS(t, "token="+reconnectToken)
	defer resumed.Close()
	e.WaitPeerCount(t, roomID, 1)
	if got := peerIDOfNick(t, e, roomID, "host"); got != hostPeerID {
		t.Fatalf("resumed peerID = %d, want %d", got, hostPeerID)
	}
}

// P31 never-joined: the room was created, but nobody entered. A live reserve
// holds it; once the slot is gone (the ticket expired — modeled here by an
// explicit Revoke, same end state), the empty-room sweep kills it.
func TestIntegration_EmptyRoom_NeverJoined_SweptAfterReserveGone(t *testing.T) {
	t.Parallel()

	e := env.New(t)
	ctx := context.Background()

	roomID, _ := e.CreateRoom(t, "host")
	// Join is not performed — the room is never-joined.

	sweep := useroom.NewSweepEmptyRoomsUseCase(e.Registry, e.Reservation, stdlib.New("test"), nil)

	// A live reserve (unexpired ticket TTL) blocks the kill.
	killed, err := sweep.RunOnce(ctx)
	if err != nil {
		t.Fatalf("empty sweep RunOnce: %v", err)
	}
	if killed != 0 {
		t.Fatalf("killed = %d, want 0 (live reserve must hold the room)", killed)
	}
	if _, err := e.Registry.GetRoom(ctx, roomID); err != nil {
		t.Fatalf("room must survive while reserve slot is live: %v", err)
	}

	// Revoke the reserve slot — the same end state as an expired ticket
	// (sweepExpiredLocked would have reaped it on the next Reserve).
	slots, err := e.Reservation.ListRoomSlots(ctx, roomID)
	if err != nil || len(slots) != 1 {
		t.Fatalf("ListRoomSlots = %v, %v; want 1 reserve slot", slots, err)
	}
	if slots[0].State != domain.ReservationSlotReserved {
		t.Fatalf("slot state = %v, want Reserved", slots[0].State)
	}
	if err := e.Reservation.Revoke(ctx, roomID, slots[0].PeerID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	killed, err = sweep.RunOnce(ctx)
	if err != nil {
		t.Fatalf("empty sweep RunOnce: %v", err)
	}
	if killed != 1 {
		t.Fatalf("killed = %d, want 1", killed)
	}
	if _, err := e.Registry.GetRoom(ctx, roomID); !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("GetRoom err = %v, want ErrRoomNotFound", err)
	}
	if listRoomIDs(t, e)[int64(roomID)] {
		t.Fatal("room still present in GET /rooms after never-joined sweep kill")
	}
}

// P31: DELETE /rooms/:id — the same closeRoom path as policy-death:
// kicked peers get 0x82 OpRoomClosed before disconnect, the room is
// unregistered from the registry and reservation.
func TestIntegration_Delete_PeerGetsRoomClosed(t *testing.T) {
	t.Parallel()

	e := env.New(t)
	ctx := context.Background()

	roomID, hostToken := e.CreateRoom(t, "host")
	hostConn := e.DialWS(t, "token="+hostToken)
	e.WaitPeerCount(t, roomID, 1)

	resp, body := e.Do(t, http.MethodDelete, "/rooms/"+strconv.FormatInt(int64(roomID), 10), nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, body = %s", resp.StatusCode, body)
	}

	payload := waitOp(t, hostConn, domain.OpRoomClosed, 3*time.Second)
	if len(payload) != 0 {
		t.Fatalf("OpRoomClosed payload = %x, want empty", payload)
	}
	_ = hostConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := hostConn.ReadMessage(); err == nil {
		t.Fatal("host read after OpRoomClosed: want disconnect error")
	}
	hostConn.Close()

	if _, err := e.Registry.GetRoom(ctx, roomID); !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("GetRoom err = %v, want ErrRoomNotFound", err)
	}
	if _, err := e.Reservation.ListAdmittedPeers(ctx, roomID); !errors.Is(err, domain.ErrReservationNotFound) {
		t.Fatalf("ListAdmittedPeers err = %v, want ErrReservationNotFound", err)
	}
}
