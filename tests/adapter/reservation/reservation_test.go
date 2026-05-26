package reservation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	adapterreservation "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/reservation"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type discardLogger struct{}

func (discardLogger) Error(string, ...any) {}
func (discardLogger) Info(string, ...any)  {}
func (discardLogger) Debug(string, ...any) {}
func (discardLogger) Warn(string, ...any)  {}
func (discardLogger) Fatal(string, ...any) {}

func newTestReservation() *adapterreservation.Reservation {
	return adapterreservation.NewReservation(discardLogger{})
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

	if err := res.Reserve(ctx, roomID, peerID, expires); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := res.Admit(ctx, roomID, peerID); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if err := res.Revoke(ctx, roomID, peerID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := res.Reserve(ctx, roomID, peerID, expires); err != nil {
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

	if err := res.Reserve(ctx, roomID, expired, past); err != nil {
		t.Fatalf("Reserve expired peer: %v", err)
	}
	if err := res.Reserve(ctx, roomID, active, future); err != nil {
		t.Fatalf("Reserve active peer: %v", err)
	}
	if err := res.Admit(ctx, roomID, expired); !errors.Is(err, domain.ErrReservationNotReserved) {
		t.Fatalf("Admit expired peer err = %v, want ErrReservationNotReserved", err)
	}
	if err := res.Admit(ctx, roomID, active); err != nil {
		t.Fatalf("Admit active peer: %v", err)
	}
}

func TestAdmit_IdempotentWhenAlreadyAdmitted(t *testing.T) {
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

	if err := res.Reserve(ctx, roomID, peerID, expires); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if err := res.Admit(ctx, roomID, peerID); err != nil {
		t.Fatalf("Admit first: %v", err)
	}
	if err := res.Admit(ctx, roomID, peerID); err != nil {
		t.Fatalf("Admit second: %v", err)
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

	if err := res.Reserve(ctx, roomID, peerID, time.Now().Add(-time.Minute)); err != nil {
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

	if err := res.Reserve(ctx, roomID, admitted, future); err != nil {
		t.Fatalf("Reserve admitted peer: %v", err)
	}
	if err := res.Admit(ctx, roomID, admitted); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if err := res.Reserve(ctx, roomID, reserved, future); err != nil {
		t.Fatalf("Reserve second peer: %v", err)
	}
	if err := res.Reserve(ctx, roomID, overflow, future); !errors.Is(err, domain.ErrReservationFull) {
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
	if err := res.Reserve(ctx, roomID, peerID, expires); err != nil {
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
	if err := res.Reserve(ctx, roomID, peerID, expires); err != nil {
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
