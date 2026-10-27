package room_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
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
	// P31: проверка «пустая» после изъятия — в комнате остаётся peer,
	// closeRoomIfEmpty no-op.
	room.EXPECT().GetPeers().Return([]realtime.Peer{mocks.NewMockPeer(ctrl)})
	logger.EXPECT().Info(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())

	uc := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	if err := uc.LeaveRoom(context.Background(), roomID, peerID); err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
}

// P31: ушёл последний peer и grace-слотов не осталось — комната умирает
// сразу (DeleteRoom + UnregisterRoom), а не ждёт DELETE/shutdown.
func TestLeaveRoom_LastPeerLeaves_RoomDiesEmpty(t *testing.T) {
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

	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return(nil)
	reservation.EXPECT().ListAdmittedPeers(gomock.Any(), roomID).Return(nil, nil)
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	logger.EXPECT().Warn(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

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
	// P31: проверка «пустая» — комната не пуста, closeRoomIfEmpty no-op.
	room.EXPECT().GetPeers().Return([]realtime.Peer{mocks.NewMockPeer(ctrl)})
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

// P29: room.Leave отвечает ErrRoomShouldClose (authoritative-host, ушёл
// master) → teardown: OpRoomClosed оставшимся → stop всех → DeleteRoom →
// UnregisterRoom. Отдельный Revoke ушедшего не нужен — слоты ревокает
// teardown.
func TestLeaveRoom_RoomShouldClose_ClosesRoom(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	master := mocks.NewMockPeer(ctrl)
	survivor := mocks.NewMockPeer(ctrl)

	const (
		roomID     = domain.RoomID(1)
		masterID   = domain.PeerID(2)
		survivorID = domain.PeerID(3)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().HasPeer(masterID).Return(true)
	room.EXPECT().GetPeer(masterID).Return(master, nil)
	room.EXPECT().Leave(master).Return(domain.ErrRoomShouldClose)
	master.EXPECT().Stop().Return(nil)

	room.EXPECT().GetID().Return(roomID).AnyTimes()
	room.EXPECT().GetPeers().Return([]realtime.Peer{survivor})
	survivor.EXPECT().GetID().Return(survivorID).AnyTimes()
	survivor.EXPECT().Deliver(gomock.Any()).DoAndReturn(func(ev events.PeerEvent) error {
		if ev.Frame.OpCode != domain.OpRoomClosed {
			t.Fatalf("delivered opcode = %#x, want OpRoomClosed (0x82)", ev.Frame.OpCode)
		}
		return nil
	})
	survivor.EXPECT().Stop().Return(nil)
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	// Revoke ушедшего master'а не вызывается — UnregisterRoom ревокает все слоты.
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewLeaveRoomUseCase(registry, reservation, logger)
	if err := uc.LeaveRoom(context.Background(), roomID, masterID); err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
}
