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

func TestDelete_Success(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	const roomID = domain.RoomID(3)
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)

	uc := useroom.NewDeleteUseCase(registry, logger)
	if err := uc.Delete(context.Background(), roomID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	registry.EXPECT().
		DeleteRoom(gomock.Any(), domain.RoomID(99)).
		Return(domain.ErrRoomNotFound)

	uc := useroom.NewDeleteUseCase(registry, logger)
	err := uc.Delete(context.Background(), domain.RoomID(99))
	if !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("err = %v, want ErrRoomNotFound", err)
	}
}
