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

// listRoomIDs возвращает множество roomID из GET /rooms.
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

// P31 emptied-death: disconnect последнего peer'а оставляет комнату живой
// в grace (слот admitted держит её — resume возможен); когда orphan sweep
// отзывает последний grace-слот, комната умирает — исчезает из registry
// и из GET /rooms.
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

	// Grace ещё держит комнату: слот admitted, peers нет — не «пустая».
	if _, err := e.Registry.GetRoom(ctx, roomID); err != nil {
		t.Fatalf("room must survive disconnect within grace: %v", err)
	}
	slots, err := e.Reservation.ListAdmittedPeers(ctx, roomID)
	if err != nil || len(slots) != 1 {
		t.Fatalf("admitted slots after disconnect = %v, %v; want 1 grace slot", slots, err)
	}

	// TTL=0: graceExpired истинно сразу — sweep ревокает последний слот.
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

// P31: disconnect последнего peer'а в пределах TTL НЕ убивает комнату —
// resume reconnect-токеном работает (регрессия: убить по голому peers==0
// значило бы сломать P20-resume — возвращаться было бы некуда).
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

	// Комната жива: grace-слот admitted держит её.
	if _, err := e.Registry.GetRoom(ctx, roomID); err != nil {
		t.Fatalf("room must survive last-peer disconnect within TTL: %v", err)
	}

	// Resume под тем же peerID — комната принимает reconnect.
	resumed := e.DialWS(t, "token="+reconnectToken)
	defer resumed.Close()
	e.WaitPeerCount(t, roomID, 1)
	if got := peerIDOfNick(t, e, roomID, "host"); got != hostPeerID {
		t.Fatalf("resumed peerID = %d, want %d", got, hostPeerID)
	}
}

// P31 never-joined: комната создана, но никто не вошёл. Живой reserve
// держит её; когда слота нет (ticket истёк — здесь моделируется явным
// Revoke, тот же конечный state), sweep пустых комнат убивает её.
func TestIntegration_EmptyRoom_NeverJoined_SweptAfterReserveGone(t *testing.T) {
	t.Parallel()

	e := env.New(t)
	ctx := context.Background()

	roomID, _ := e.CreateRoom(t, "host")
	// Join не выполняется — комната never-joined.

	sweep := useroom.NewSweepEmptyRoomsUseCase(e.Registry, e.Reservation, stdlib.New("test"), nil)

	// Живой reserve (unexpired ticket TTL) блокирует kill.
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

	// Отзываем reserve-слот — тот же конечный state, что у истёкшего
	// ticket'а (sweepExpiredLocked вычистил бы его при следующем Reserve).
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

// P31: DELETE /rooms/:id — тот же closeRoom-путь, что и policy-death:
// kicked peers получают 0x82 OpRoomClosed до disconnect, комната
// unregister'ится из registry и reservation.
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
