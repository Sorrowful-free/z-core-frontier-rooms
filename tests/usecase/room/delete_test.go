package room_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

func newDeleteUseCase(
	t *testing.T,
	registry *mocks.MockRoomRegistry,
	reservation *mocks.MockReservation,
	leaveRoom *useroom.LeaveRoomUseCase,
	logger *mocks.MockLogger,
) *useroom.DeleteUseCase {
	t.Helper()
	return useroom.NewDeleteUseCase(registry, reservation, leaveRoom, logger)
}

func TestDelete_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const roomID = domain.RoomID(3)
	gomock.InOrder(
		reservation.EXPECT().VerifyRoomPassword(gomock.Any(), roomID, "room-secret").Return(nil),
		registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil),
	)
	room.EXPECT().GetPeers().Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	leaveUC := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	uc := newDeleteUseCase(t, registry, reservation, leaveUC, logger)
	if err := uc.Delete(context.Background(), roomID, "room-secret"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestDelete_KicksPeersBeforeDelete(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peerA := mocks.NewMockPeer(ctrl)
	peerB := mocks.NewMockPeer(ctrl)

	const (
		roomID  = domain.RoomID(4)
		peerIDA = domain.PeerID(10)
		peerIDB = domain.PeerID(11)
	)

	reservation.EXPECT().VerifyRoomPassword(gomock.Any(), roomID, "").Return(nil)
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().GetPeers().Return([]realtime.Peer{peerA, peerB})
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	peerA.EXPECT().GetID().Return(peerIDA).AnyTimes()
	peerB.EXPECT().GetID().Return(peerIDB).AnyTimes()

	gomock.InOrder(
		registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil),
		room.EXPECT().GetPeer(peerIDA).Return(peerA, nil),
		room.EXPECT().Leave(peerA).Return(nil),
		peerA.EXPECT().Stop().Return(nil),
		reservation.EXPECT().Revoke(gomock.Any(), roomID, peerIDA).Return(nil),
		logger.EXPECT().Info(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()),

		registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil),
		room.EXPECT().GetPeer(peerIDB).Return(peerB, nil),
		room.EXPECT().Leave(peerB).Return(nil),
		peerB.EXPECT().Stop().Return(nil),
		reservation.EXPECT().Revoke(gomock.Any(), roomID, peerIDB).Return(nil),
		logger.EXPECT().Info(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()),
	)

	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	leaveUC := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	uc := newDeleteUseCase(t, registry, reservation, leaveUC, logger)
	if err := uc.Delete(context.Background(), roomID, ""); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	reservation.EXPECT().
		VerifyRoomPassword(gomock.Any(), domain.RoomID(99), "").
		Return(domain.ErrReservationNotFound)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	leaveUC := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	uc := newDeleteUseCase(t, registry, reservation, leaveUC, logger)
	err := uc.Delete(context.Background(), domain.RoomID(99), "")
	if !errors.Is(err, domain.ErrReservationNotFound) {
		t.Fatalf("err = %v, want ErrReservationNotFound", err)
	}
}

func TestDelete_UnregisterAfterRegistrySuccess(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const roomID = domain.RoomID(5)
	reservation.EXPECT().VerifyRoomPassword(gomock.Any(), roomID, "").Return(nil)
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().GetPeers().Return(nil)
	room.EXPECT().GetID().Return(roomID).AnyTimes()
	gomock.InOrder(
		registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil),
		reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(domain.ErrReservationNotFound),
	)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	leaveUC := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	uc := newDeleteUseCase(t, registry, reservation, leaveUC, logger)
	err := uc.Delete(context.Background(), roomID, "")
	if !errors.Is(err, domain.ErrReservationNotFound) {
		t.Fatalf("err = %v, want ErrReservationNotFound", err)
	}
}

func TestDelete_InvalidPassword(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	const roomID = domain.RoomID(8)
	reservation.EXPECT().
		VerifyRoomPassword(gomock.Any(), roomID, "bad").
		Return(domain.ErrInvalidCredentials)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	leaveUC := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	uc := newDeleteUseCase(t, registry, reservation, leaveUC, logger)
	err := uc.Delete(context.Background(), roomID, "bad")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}
