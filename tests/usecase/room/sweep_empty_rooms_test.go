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

// P31: never-joined комната — reserve истёк → sweep убивает комнату.
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
	// GetPeers вызывается дважды: предикат sweep'а и перепроверка
	// closeRoomIfEmpty перед kill.
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

// P31: never-joined комната — живой reserve (unexpired ticket) держит
// комнату: join ещё может состояться, kill запрещён.
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
	// DeleteRoom/UnregisterRoom не ожидаются — gomock упадёт на вызове.

	uc := useroom.NewSweepEmptyRoomsUseCase(registry, reservation, logger, nil)
	killed, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if killed != 0 {
		t.Fatalf("killed = %d, want 0", killed)
	}
}

// P31: admitted grace-слот держит комнату — reconnect ещё возможен.
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
		LastSeen: time.Now().Add(-time.Hour), // даже «старый» grace не sweep'ится здесь —
		// его дедлайн разбирает orphan-sweep; пока слот admitted, он живой.
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

// P31: комната без единого слота (reserve истёк и вычислен лениво раньше)
// — kill.
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

// P31: reservation уже unregister'нута параллельным teardown → слотов нет
// → kill комнаты-трупа в registry.
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

// P31: комната с живыми peers пропускается без листинга слотов.
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
	// ListRoomSlots не ожидается — peers > 0, ранний выход.

	uc := useroom.NewSweepEmptyRoomsUseCase(registry, reservation, logger, nil)
	killed, err := uc.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if killed != 0 {
		t.Fatalf("killed = %d, want 0", killed)
	}
}
