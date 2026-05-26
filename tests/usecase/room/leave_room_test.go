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
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)
	peer := mocks.NewMockPeer(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().GetPeer(peerID).Return(peer, nil)
	room.EXPECT().Leave(peer).Return(nil)
	peer.EXPECT().Stop().Return(nil)
	logger.EXPECT().Info(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())

	uc := useroom.NewLeaveRoomUseCase(registry, logger)
	if err := uc.LeaveRoom(context.Background(), roomID, peerID); err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
}

func TestLeaveRoom_RoomNotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	registry.EXPECT().
		GetRoom(gomock.Any(), domain.RoomID(1)).
		Return(nil, domain.ErrRoomNotFound)

	uc := useroom.NewLeaveRoomUseCase(registry, logger)
	err := uc.LeaveRoom(context.Background(), domain.RoomID(1), domain.PeerID(2))
	if !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("err = %v, want ErrRoomNotFound", err)
	}
}

func TestLeaveRoom_PeerNotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID = domain.RoomID(1)
		peerID = domain.PeerID(2)
	)

	registry.EXPECT().GetRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().GetPeer(peerID).Return(nil, errors.New("peer not found"))

	uc := useroom.NewLeaveRoomUseCase(registry, logger)
	if err := uc.LeaveRoom(context.Background(), roomID, peerID); err == nil {
		t.Fatal("LeaveRoom: want error")
	}
}
