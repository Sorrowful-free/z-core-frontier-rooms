package room_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
	"go.uber.org/mock/gomock"
)

func TestLeaveRoom_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().HasPeer(peerID).Return(true)
	room.EXPECT().GetPeer(peerID).Return(peer, nil)
	room.EXPECT().Leave(peer).Return(nil)
	peer.EXPECT().Stop().Return(nil)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)
	logger.EXPECT().Info(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())

	uc := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	if err := uc.LeaveRoom(context.Background(), roomID, peerID); err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
}

func TestLeaveRoom_PeerAlreadyLeft(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().HasPeer(peerID).Return(false)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)
	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	if err := uc.LeaveRoom(context.Background(), roomID, peerID); err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
}

func TestLeaveRoom_RoomNotFound_RevokesAndSucceeds(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().
		GetRoom(gomock.Any(), roomID).
		Return(nil, domain.ErrRoomNotFound)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)
	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	if err := uc.LeaveRoom(context.Background(), roomID, peerID); err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
}

func TestLeaveRoom_GetPeerFails(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().HasPeer(peerID).Return(true)
	room.EXPECT().GetPeer(peerID).Return(nil, errors.New("peer not found"))

	uc := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	if err := uc.LeaveRoom(context.Background(), roomID, peerID); err == nil {
		t.Fatal("LeaveRoom: want error")
	}
}

func TestLeaveRoom_LeaveFails_StillStopsPeerAndRevokes(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	leaveErr := errors.New("leave failed")

	// room.Leave изымает peer из комнаты даже при ошибке политики:
	// peer.Stop и Revoke обязаны выполниться, ошибка leave — вернуться.
	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().HasPeer(peerID).Return(true)
	room.EXPECT().GetPeer(peerID).Return(peer, nil)
	room.EXPECT().Leave(peer).Return(leaveErr)
	peer.EXPECT().Stop().Return(nil)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	err := uc.LeaveRoom(context.Background(), roomID, peerID)
	if !errors.Is(err, leaveErr) {
		t.Fatalf("err = %v, want errors.Is leaveErr", err)
	}
}

func TestLeaveRoom_RevokeNotFoundStillSuccess(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().HasPeer(peerID).Return(true)
	room.EXPECT().GetPeer(peerID).Return(peer, nil)
	room.EXPECT().Leave(peer).Return(nil)
	peer.EXPECT().Stop().Return(nil)
	reservation.EXPECT().
		Revoke(gomock.Any(), roomID, peerID).
		Return(domain.ErrReservationNotFound)
	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())

	uc := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	if err := uc.LeaveRoom(context.Background(), roomID, peerID); err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
}

func TestLeaveRoom_PeerStopFails_StillRevokesReservation(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	stopErr := errors.New("peer stop failed")

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().HasPeer(peerID).Return(true)
	room.EXPECT().GetPeer(peerID).Return(peer, nil)
	room.EXPECT().Leave(peer).Return(nil)
	peer.EXPECT().Stop().Return(stopErr)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	err := uc.LeaveRoom(context.Background(), roomID, peerID)
	if !errors.Is(err, stopErr) {
		t.Fatalf("err = %v, want errors.Is stopErr", err)
	}
}

func TestLeaveRoom_StopAndRevokeFail_ErrorsJoined(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)
	stopErr := errors.New("peer stop failed")
	revokeErr := errors.New("revoke failed")

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().HasPeer(peerID).Return(true)
	room.EXPECT().GetPeer(peerID).Return(peer, nil)
	room.EXPECT().Leave(peer).Return(nil)
	peer.EXPECT().Stop().Return(stopErr)
	reservation.EXPECT().Revoke(gomock.Any(), roomID, peerID).Return(revokeErr)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	err := uc.LeaveRoom(context.Background(), roomID, peerID)
	if !errors.Is(err, stopErr) {
		t.Fatalf("err = %v, want errors.Is stopErr", err)
	}
	if !errors.Is(err, revokeErr) {
		t.Fatalf("err = %v, want errors.Is revokeErr", err)
	}
}
