package reservation_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	adapterreservation "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/reservation"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func testNick(id domain.PeerID) string {
	return fmt.Sprintf("peer-%d", id)
}

type discardLogger struct{}

func (discardLogger) Error(string, ...any) {}
func (discardLogger) Info(string, ...any)  {}
func (discardLogger) Debug(string, ...any) {}
func (discardLogger) Warn(string, ...any)  {}
func (discardLogger) Fatal(string, ...any) {}

func newTestReservation() *adapterreservation.Reservation {
	return adapterreservation.NewReservation(discardLogger{}, nil)
}

func TestReserveAdmitRevoke_ReleasesSlot(t *testing.T) {
	t.Parallel()

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(10)
	)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 1, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}
	expires := time.Now().Add(time.Hour)

	if err := res.Reserve(ctx, roomID, peerID, testNick(peerID), expires); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := res.Admit(ctx, roomID, peerID); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if err := res.Revoke(ctx, roomID, peerID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := res.Reserve(ctx, roomID, peerID, testNick(peerID), expires); err != nil {
		t.Fatalf("Reserve after Revoke: %v", err)
	}
}

func TestReserve_ExpiredRemovedOnNextReserve(t *testing.T) {
	t.Parallel()

	const (
		roomID  = domain.RoomID(2)
		expired = domain.PeerID(20)
		active  = domain.PeerID(21)
	)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 2, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	if err := res.Reserve(ctx, roomID, expired, testNick(expired), past); err != nil {
		t.Fatalf("Reserve expired peer: %v", err)
	}
	if err := res.Reserve(ctx, roomID, active, testNick(active), future); err != nil {
		t.Fatalf("Reserve active peer: %v", err)
	}
	if err := res.Admit(ctx, roomID, expired); !errors.Is(err, domain.ErrReservationNotReserved) {
		t.Fatalf("Admit expired peer err = %v, want ErrReservationNotReserved", err)
	}
	if err := res.Admit(ctx, roomID, active); err != nil {
		t.Fatalf("Admit active peer: %v", err)
	}
}

// D3: the ticket is single-use — a repeated Admit on an admitted slot is rejected.
func TestAdmit_AlreadyAdmittedRejected(t *testing.T) {
	t.Parallel()

	const (
		roomID = domain.RoomID(3)
		peerID = domain.PeerID(30)
	)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 1, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}
	expires := time.Now().Add(time.Hour)

	if err := res.Reserve(ctx, roomID, peerID, testNick(peerID), expires); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := res.Admit(ctx, roomID, peerID); err != nil {
		t.Fatalf("Admit first: %v", err)
	}
	if err := res.Admit(ctx, roomID, peerID); !errors.Is(err, domain.ErrReservationAlreadyAdmitted) {
		t.Fatalf("Admit second err = %v, want ErrReservationAlreadyAdmitted", err)
	}
}

// D5: one outstanding reserved slot per nick in a room; admitted does not block.
func TestReserve_SameNickSlotHeld(t *testing.T) {
	t.Parallel()

	const (
		roomID = domain.RoomID(11)
		peerA  = domain.PeerID(110)
		peerB  = domain.PeerID(111)
		peerC  = domain.PeerID(112)
		nick   = "alex"
	)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 3, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}
	expires := time.Now().Add(time.Hour)

	if err := res.Reserve(ctx, roomID, peerA, nick, expires); err != nil {
		t.Fatalf("Reserve peerA: %v", err)
	}
	if err := res.Reserve(ctx, roomID, peerB, nick, expires); !errors.Is(err, domain.ErrTicketSlotHeld) {
		t.Fatalf("Reserve same nick err = %v, want ErrTicketSlotHeld", err)
	}
	if err := res.Admit(ctx, roomID, peerA); err != nil {
		t.Fatalf("Admit peerA: %v", err)
	}
	if err := res.Reserve(ctx, roomID, peerC, nick, expires); err != nil {
		t.Fatalf("Reserve same nick after admit (reconnect): %v", err)
	}
}

func TestAdmit_ExpiredReservation(t *testing.T) {
	t.Parallel()

	const (
		roomID = domain.RoomID(4)
		peerID = domain.PeerID(40)
	)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 1, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}

	if err := res.Reserve(ctx, roomID, peerID, testNick(peerID), time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	err := res.Admit(ctx, roomID, peerID)
	if !errors.Is(err, domain.ErrReservationExpired) {
		t.Fatalf("Admit err = %v, want ErrReservationExpired", err)
	}
	if err := res.Admit(ctx, roomID, peerID); !errors.Is(err, domain.ErrReservationNotReserved) {
		t.Fatalf("Admit after expiry cleanup err = %v, want ErrReservationNotReserved", err)
	}
}

func TestSweep_DoesNotRemoveAdmitted(t *testing.T) {
	t.Parallel()

	const (
		roomID   = domain.RoomID(5)
		admitted = domain.PeerID(50)
		reserved = domain.PeerID(51)
		overflow = domain.PeerID(52)
	)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 2, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}
	future := time.Now().Add(time.Hour)

	if err := res.Reserve(ctx, roomID, admitted, testNick(admitted), future); err != nil {
		t.Fatalf("Reserve admitted peer: %v", err)
	}
	if err := res.Admit(ctx, roomID, admitted); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if err := res.Reserve(ctx, roomID, reserved, testNick(reserved), future); err != nil {
		t.Fatalf("Reserve second peer: %v", err)
	}
	if err := res.Reserve(ctx, roomID, overflow, testNick(overflow), future); !errors.Is(err, domain.ErrReservationFull) {
		t.Fatalf("Reserve overflow err = %v, want ErrReservationFull", err)
	}
}

func TestRevoke_IdempotentWhenPeerMissing(t *testing.T) {
	t.Parallel()

	const (
		roomID = domain.RoomID(6)
		peerID = domain.PeerID(60)
	)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 1, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}
	if err := res.Revoke(ctx, roomID, peerID); err != nil {
		t.Fatalf("Revoke missing peer: %v", err)
	}

	expires := time.Now().Add(time.Hour)
	if err := res.Reserve(ctx, roomID, peerID, testNick(peerID), expires); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := res.Revoke(ctx, roomID, peerID); err != nil {
		t.Fatalf("Revoke first: %v", err)
	}
	if err := res.Revoke(ctx, roomID, peerID); err != nil {
		t.Fatalf("Revoke second (idempotent): %v", err)
	}
}

func TestRevoke_RoomNotRegistered(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	res := newTestReservation()

	err := res.Revoke(ctx, domain.RoomID(99), domain.PeerID(1))
	if !errors.Is(err, domain.ErrReservationNotFound) {
		t.Fatalf("Revoke err = %v, want ErrReservationNotFound", err)
	}
}

func TestState(t *testing.T) {
	t.Parallel()

	const (
		roomID = domain.RoomID(7)
		peerID = domain.PeerID(70)
	)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 1, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}

	state, err := res.State(ctx, roomID, peerID)
	if err != nil {
		t.Fatalf("State missing peer: %v", err)
	}
	if state != domain.ReservationSlotNone {
		t.Fatalf("state = %v, want None", state)
	}

	expires := time.Now().Add(time.Hour)
	if err := res.Reserve(ctx, roomID, peerID, testNick(peerID), expires); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	state, err = res.State(ctx, roomID, peerID)
	if err != nil {
		t.Fatalf("State reserved: %v", err)
	}
	if state != domain.ReservationSlotReserved {
		t.Fatalf("state = %v, want Reserved", state)
	}

	if err := res.Admit(ctx, roomID, peerID); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	state, err = res.State(ctx, roomID, peerID)
	if err != nil {
		t.Fatalf("State admitted: %v", err)
	}
	if state != domain.ReservationSlotAdmitted {
		t.Fatalf("state = %v, want Admitted", state)
	}
}

func TestListAdmittedPeers(t *testing.T) {
	t.Parallel()

	const (
		roomID   = domain.RoomID(10)
		admitted = domain.PeerID(100)
		reserved = domain.PeerID(101)
	)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 2, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}
	expires := time.Now().Add(time.Hour)

	if err := res.Reserve(ctx, roomID, admitted, testNick(admitted), expires); err != nil {
		t.Fatalf("Reserve admitted: %v", err)
	}
	if err := res.Admit(ctx, roomID, admitted); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if err := res.Reserve(ctx, roomID, reserved, testNick(reserved), expires); err != nil {
		t.Fatalf("Reserve reserved: %v", err)
	}

	slots, err := res.ListAdmittedPeers(ctx, roomID)
	if err != nil {
		t.Fatalf("ListAdmittedPeers: %v", err)
	}
	if len(slots) != 1 {
		t.Fatalf("len(slots) = %d, want 1", len(slots))
	}
	if slots[0].PeerID != admitted {
		t.Fatalf("peerID = %v, want %v", slots[0].PeerID, admitted)
	}
	if slots[0].LastSeen.IsZero() {
		t.Fatal("lastSeen is zero")
	}
}

func TestVerifyRoomPassword(t *testing.T) {
	t.Parallel()

	const roomID = domain.RoomID(8)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 1, "secret"); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}
	if err := res.VerifyRoomPassword(ctx, roomID, "secret"); err != nil {
		t.Fatalf("Verify correct password: %v", err)
	}
	if err := res.VerifyRoomPassword(ctx, roomID, "wrong"); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("Verify wrong password err = %v, want ErrInvalidCredentials", err)
	}

	if err := res.RegisterRoom(ctx, domain.RoomID(9), 1, ""); err != nil {
		t.Fatalf("RegisterRoom open: %v", err)
	}
	if err := res.VerifyRoomPassword(ctx, domain.RoomID(9), ""); err != nil {
		t.Fatalf("Verify open room empty password: %v", err)
	}
	if err := res.VerifyRoomPassword(ctx, domain.RoomID(9), "anything"); err != nil {
		t.Fatalf("Verify open room any password: %v", err)
	}
}

func TestReservation_ConcurrentDifferentRooms(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	res := newTestReservation()
	const rooms = 32

	for id := domain.RoomID(1); id <= rooms; id++ {
		if err := res.RegisterRoom(ctx, id, 8, ""); err != nil {
			t.Fatalf("RegisterRoom %d: %v", id, err)
		}
	}

	expires := time.Now().Add(time.Hour)
	var wg sync.WaitGroup
	for id := domain.RoomID(1); id <= rooms; id++ {
		wg.Add(1)
		go func(roomID domain.RoomID) {
			defer wg.Done()
			peerID := domain.PeerID(roomID * 100)
			if err := res.Reserve(ctx, roomID, peerID, testNick(peerID), expires); err != nil {
				t.Errorf("Reserve room %d: %v", roomID, err)
				return
			}
			if err := res.Admit(ctx, roomID, peerID); err != nil {
				t.Errorf("Admit room %d: %v", roomID, err)
			}
		}(id)
	}
	wg.Wait()
}

// P20: MarkDisconnected keeps the admitted slot (grace), refreshes lastSeen.
func TestMarkDisconnected_KeepsAdmittedSlot(t *testing.T) {
	t.Parallel()

	const (
		roomID = domain.RoomID(20)
		peerID = domain.PeerID(200)
	)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 1, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}
	if err := res.Reserve(ctx, roomID, peerID, testNick(peerID), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := res.Admit(ctx, roomID, peerID); err != nil {
		t.Fatalf("Admit: %v", err)
	}

	before := time.Now()
	if err := res.MarkDisconnected(ctx, roomID, peerID); err != nil {
		t.Fatalf("MarkDisconnected: %v", err)
	}

	// The slot is alive and admitted — capacity is still taken.
	if err := res.Reserve(ctx, roomID, domain.PeerID(201), "other", time.Now().Add(time.Hour)); !errors.Is(err, domain.ErrReservationFull) {
		t.Fatalf("Reserve after mark err = %v, want ErrReservationFull (slot is in grace)", err)
	}

	slots, err := res.ListAdmittedPeers(ctx, roomID)
	if err != nil {
		t.Fatalf("ListAdmittedPeers: %v", err)
	}
	if len(slots) != 1 || slots[0].PeerID != peerID {
		t.Fatalf("slots = %+v", slots)
	}
	if slots[0].LastSeen.Before(before) {
		t.Fatalf("lastSeen %v before mark %v", slots[0].LastSeen, before)
	}
	if slots[0].NickName != testNick(peerID) {
		t.Fatalf("slot nick = %q, want %q", slots[0].NickName, testNick(peerID))
	}
}

func TestMarkDisconnected_NotAdmitted(t *testing.T) {
	t.Parallel()

	const roomID = domain.RoomID(21)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 2, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}
	if err := res.MarkDisconnected(ctx, roomID, domain.PeerID(1)); !errors.Is(err, domain.ErrReservationNotReserved) {
		t.Fatalf("mark missing peer err = %v, want ErrReservationNotReserved", err)
	}
	if err := res.Reserve(ctx, roomID, domain.PeerID(1), "p1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := res.MarkDisconnected(ctx, roomID, domain.PeerID(1)); !errors.Is(err, domain.ErrReservationNotReserved) {
		t.Fatalf("mark reserved peer err = %v, want ErrReservationNotReserved", err)
	}
}

// P20: Reconnect — atomic admitted check + lastSeen refresh + nick.
func TestReconnect_ReturnsSlotAndRefreshes(t *testing.T) {
	t.Parallel()

	const (
		roomID = domain.RoomID(22)
		peerID = domain.PeerID(220)
	)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 1, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}
	if err := res.Reserve(ctx, roomID, peerID, testNick(peerID), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := res.Admit(ctx, roomID, peerID); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if err := res.MarkDisconnected(ctx, roomID, peerID); err != nil {
		t.Fatalf("MarkDisconnected: %v", err)
	}

	before := time.Now()
	slot, err := res.Reconnect(ctx, roomID, peerID)
	if err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	if slot.PeerID != peerID || slot.NickName != testNick(peerID) {
		t.Fatalf("slot = %+v", slot)
	}
	if slot.LastSeen.Before(before) {
		t.Fatalf("lastSeen %v not refreshed", slot.LastSeen)
	}
}

func TestReconnect_Rejected(t *testing.T) {
	t.Parallel()

	const roomID = domain.RoomID(23)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 2, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}
	// No slot.
	if _, err := res.Reconnect(ctx, roomID, domain.PeerID(1)); !errors.Is(err, domain.ErrReservationNotReserved) {
		t.Fatalf("reconnect missing err = %v, want ErrReservationNotReserved", err)
	}
	// Slot is reserved (not yet admitted).
	if err := res.Reserve(ctx, roomID, domain.PeerID(1), "p1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if _, err := res.Reconnect(ctx, roomID, domain.PeerID(1)); !errors.Is(err, domain.ErrReservationNotReserved) {
		t.Fatalf("reconnect reserved err = %v, want ErrReservationNotReserved", err)
	}
	// Room not registered.
	if _, err := res.Reconnect(ctx, domain.RoomID(99), domain.PeerID(1)); !errors.Is(err, domain.ErrReservationNotFound) {
		t.Fatalf("reconnect bad room err = %v, want ErrReservationNotFound", err)
	}
}

// P24: SlotDetails — read-only view of an admitted slot: returns the slot
// with NickName and does NOT refresh lastSeen (honest lastSeen for the
// resume deadline gate — a rejected attempt does not extend grace).
func TestSlotDetails_ReadsAdmittedWithoutRefresh(t *testing.T) {
	t.Parallel()

	const (
		roomID = domain.RoomID(24)
		peerID = domain.PeerID(240)
	)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 1, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}
	if err := res.Reserve(ctx, roomID, peerID, testNick(peerID), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := res.Admit(ctx, roomID, peerID); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if err := res.MarkDisconnected(ctx, roomID, peerID); err != nil {
		t.Fatalf("MarkDisconnected: %v", err)
	}

	first, err := res.SlotDetails(ctx, roomID, peerID)
	if err != nil {
		t.Fatalf("SlotDetails: %v", err)
	}
	if first.PeerID != peerID || first.NickName != testNick(peerID) {
		t.Fatalf("slot = %+v", first)
	}
	if first.LastSeen.IsZero() {
		t.Fatal("lastSeen is zero")
	}

	second, err := res.SlotDetails(ctx, roomID, peerID)
	if err != nil {
		t.Fatalf("SlotDetails second: %v", err)
	}
	// Read-only: a second read does not move lastSeen (unlike Reconnect,
	// which would return a refreshed lastSeen).
	if !second.LastSeen.Equal(first.LastSeen) {
		t.Fatalf("lastSeen refreshed: %v -> %v", first.LastSeen, second.LastSeen)
	}
}

func TestSlotDetails_Rejected(t *testing.T) {
	t.Parallel()

	const roomID = domain.RoomID(25)
	ctx := context.Background()
	res := newTestReservation()

	if err := res.RegisterRoom(ctx, roomID, 2, ""); err != nil {
		t.Fatalf("RegisterRoom: %v", err)
	}
	// No slot.
	if _, err := res.SlotDetails(ctx, roomID, domain.PeerID(1)); !errors.Is(err, domain.ErrReservationNotReserved) {
		t.Fatalf("slot details missing err = %v, want ErrReservationNotReserved", err)
	}
	// Slot is reserved (not yet admitted).
	if err := res.Reserve(ctx, roomID, domain.PeerID(1), "p1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if _, err := res.SlotDetails(ctx, roomID, domain.PeerID(1)); !errors.Is(err, domain.ErrReservationNotReserved) {
		t.Fatalf("slot details reserved err = %v, want ErrReservationNotReserved", err)
	}
	// Room not registered.
	if _, err := res.SlotDetails(ctx, domain.RoomID(99), domain.PeerID(1)); !errors.Is(err, domain.ErrReservationNotFound) {
		t.Fatalf("slot details bad room err = %v, want ErrReservationNotFound", err)
	}
}
