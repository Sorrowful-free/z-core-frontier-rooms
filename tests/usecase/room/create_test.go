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
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)
	room := mocks.NewMockRoom(ctrl)

	const (
		roomID   = domain.RoomID(10)
		capacity = 8
	)
	reservation.EXPECT().RegisterRoom(gomock.Any(), roomID, capacity).Return(nil)
	registry.EXPECT().CreateRoom(gomock.Any(), roomID, capacity).Return(room, nil)
	room.EXPECT().GetPeers().Return(nil)
	room.EXPECT().GetID().Return(roomID)
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, reservation, logger)
	summary, err := uc.Create(context.Background(), roomID, capacity)
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
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	uc := useroom.NewCreateUseCase(registry, reservation, logger)
	_, err := uc.Create(ctx, domain.RoomID(1), 4)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestCreate_RegistryError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	const (
		roomID   = domain.RoomID(1)
		capacity = 4
	)

	reservation.EXPECT().RegisterRoom(gomock.Any(), roomID, capacity).Return(nil)
	registry.EXPECT().
		CreateRoom(gomock.Any(), roomID, capacity).
		Return(nil, domain.ErrRoomAlreadyExists)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, reservation, logger)
	_, err := uc.Create(context.Background(), roomID, capacity)
	if !errors.Is(err, domain.ErrRoomAlreadyExists) {
		t.Fatalf("err = %v, want ErrRoomAlreadyExists", err)
	}
}

func TestCreate_RegistryErrorJoinsUnregisterError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	registry := mocks.NewMockRoomRegistry(ctrl)
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	const (
		roomID   = domain.RoomID(1)
		capacity = 4
	)

	reservation.EXPECT().RegisterRoom(gomock.Any(), roomID, capacity).Return(nil)
	registry.EXPECT().
		CreateRoom(gomock.Any(), roomID, capacity).
		Return(nil, domain.ErrRoomAlreadyExists)
	reservation.EXPECT().
		UnregisterRoom(gomock.Any(), roomID).
		Return(domain.ErrReservationNotFound)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewCreateUseCase(registry, reservation, logger)
	_, err := uc.Create(context.Background(), roomID, capacity)
	if !errors.Is(err, domain.ErrRoomAlreadyExists) {
		t.Fatalf("err = %v, want ErrRoomAlreadyExists", err)
	}
	if !errors.Is(err, domain.ErrReservationNotFound) {
		t.Fatalf("err = %v, want joined ErrReservationNotFound", err)
	}
}
