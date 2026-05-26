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

func TestCreate_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const roomID = domain.RoomID(10)
	registry.EXPECT().CreateRoom(gomock.Any(), roomID).Return(room, nil)
	room.EXPECT().GetID().Return(roomID)
	room.EXPECT().GetPeers().Return(nil)

	uc := useroom.NewCreateUseCase(registry, logger)
	summary, err := uc.Create(context.Background(), roomID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if summary.ID != roomID {
		t.Fatalf("summary.ID = %v, want %v", summary.ID, roomID)
	}
}

func TestCreate_CancelledContext(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	uc := useroom.NewCreateUseCase(registry, logger)
	_, err := uc.Create(ctx, domain.RoomID(1))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestCreate_RegistryError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	registry.EXPECT().
		CreateRoom(gomock.Any(), domain.RoomID(1)).
		Return(nil, domain.ErrRoomAlreadyExists)

	uc := useroom.NewCreateUseCase(registry, logger)
	_, err := uc.Create(context.Background(), domain.RoomID(1))
	if !errors.Is(err, domain.ErrRoomAlreadyExists) {
		t.Fatalf("err = %v, want ErrRoomAlreadyExists", err)
	}
}
