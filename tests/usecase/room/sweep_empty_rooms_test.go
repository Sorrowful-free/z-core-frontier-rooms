package room_test

import (
	"context"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

// P31: never-joined room — the reserve expired → the sweep kills the room.
func TestSweepEmptyRooms_ExpiredReserve_KillsRoom(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(10)
	)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{room}, nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	// GetPeers is called twice: the sweep predicate and the closeRoomIfEmpty
	// recheck before the kill.
	room.EXPECT().GetPeers().Return(nil).Times(2)
	reservation.EXPECT().ListRoomSlots(gomock.Any(), roomID).Return([]domain.RoomSlotDetail{{
		PeerID:    peerID,
		State:     domain.ReservationSlotReserved,
		ExpiresAt: time.Now().Add(-time.Minute),
	}}, nil)
	reservation.EXPECT().ListAdmittedPeers(gomock.Any(), roomID).Return(nil, nil)
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewSweepEmptyRoomsUseCase(registry, reservation, logger, nil)
	killed, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if killed != 1 {
		t.Fatalf("killed = %d, want 1", killed)
	}
}

// P31: never-joined room — a live reserve (unexpired ticket) keeps the
// room alive: the join may still happen, kill is forbidden.
func TestSweepEmptyRooms_LiveReserve_SkipsRoom(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const roomID = domain.RoomID(2)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{room}, nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return(nil)
	reservation.EXPECT().ListRoomSlots(gomock.Any(), roomID).Return([]domain.RoomSlotDetail{{
		PeerID:    domain.PeerID(20),
		State:     domain.ReservationSlotReserved,
		ExpiresAt: time.Now().Add(time.Hour),
	}}, nil)
	// DeleteRoom/UnregisterRoom are not expected — gomock fails on a call.

	uc := useroom.NewSweepEmptyRoomsUseCase(registry, reservation, logger, nil)
	killed, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if killed != 0 {
		t.Fatalf("killed = %d, want 0", killed)
	}
}

// P31: an admitted grace slot keeps the room alive — a reconnect is still possible.
func TestSweepEmptyRooms_AdmittedGrace_SkipsRoom(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const roomID = domain.RoomID(3)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{room}, nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return(nil)
	reservation.EXPECT().ListRoomSlots(gomock.Any(), roomID).Return([]domain.RoomSlotDetail{{
		PeerID:   domain.PeerID(30),
		State:    domain.ReservationSlotAdmitted,
		LastSeen: time.Now().Add(-time.Hour), // even a "stale" grace is not swept here —
		// its deadline is handled by the orphan sweep; while the slot is admitted, it is alive.
	}}, nil)

	uc := useroom.NewSweepEmptyRoomsUseCase(registry, reservation, logger, nil)
	killed, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if killed != 0 {
		t.Fatalf("killed = %d, want 0", killed)
	}
}

// P31: a room without a single slot (the reserve expired and was lazily
// reaped earlier) — kill.
func TestSweepEmptyRooms_NoSlots_KillsRoom(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const roomID = domain.RoomID(4)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{room}, nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return(nil).Times(2)
	reservation.EXPECT().ListRoomSlots(gomock.Any(), roomID).Return(nil, nil)
	reservation.EXPECT().ListAdmittedPeers(gomock.Any(), roomID).Return(nil, nil)
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewSweepEmptyRoomsUseCase(registry, reservation, logger, nil)
	killed, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if killed != 1 {
		t.Fatalf("killed = %d, want 1", killed)
	}
}

// P31: the reservation was already unregistered by a parallel teardown → no
// slots → kill the dead room in the registry.
func TestSweepEmptyRooms_ReservationGone_KillsRoom(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const roomID = domain.RoomID(5)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{room}, nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return(nil).Times(2)
	reservation.EXPECT().ListRoomSlots(gomock.Any(), roomID).Return(nil, domain.ErrReservationNotFound)
	reservation.EXPECT().ListAdmittedPeers(gomock.Any(), roomID).Return(nil, domain.ErrReservationNotFound)
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(domain.ErrReservationNotFound)
	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewSweepEmptyRoomsUseCase(registry, reservation, logger, nil)
	killed, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if killed != 1 {
		t.Fatalf("killed = %d, want 1", killed)
	}
}

// P31: a room with live peers is skipped without listing the slots.
func TestSweepEmptyRooms_RoomWithPeers_Skipped(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)

	registry.EXPECT().GetList(gomock.Any()).Return([]realtime.Room{room}, nil)
	room.EXPECT().GetPeers().Return([]realtime.Peer{peer})
	// ListRoomSlots is not expected — peers > 0, early exit.

	uc := useroom.NewSweepEmptyRoomsUseCase(registry, reservation, logger, nil)
	killed, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if killed != 0 {
		t.Fatalf("killed = %d, want 0", killed)
	}
}
