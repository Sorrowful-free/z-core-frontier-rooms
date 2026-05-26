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
	reservation := mocks.NewMockReservation(ctrl)
	logger := mocks.NewMockLogger(ctrl)

	const roomID = domain.RoomID(3)
	reservation.EXPECT().VerifyRoomPassword(gomock.Any(), roomID, "room-secret").Return(nil)
	registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil)
	reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(nil)
	logger.EXPECT().Info(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewDeleteUseCase(registry, reservation, logger)
	if err := uc.Delete(context.Background(), roomID, "room-secret"); err != nil {
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

	uc := useroom.NewDeleteUseCase(registry, reservation, logger)
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

	const roomID = domain.RoomID(5)
	reservation.EXPECT().VerifyRoomPassword(gomock.Any(), roomID, "").Return(nil)
	gomock.InOrder(
		registry.EXPECT().DeleteRoom(gomock.Any(), roomID).Return(nil),
		reservation.EXPECT().UnregisterRoom(gomock.Any(), roomID).Return(domain.ErrReservationNotFound),
	)
	logger.EXPECT().Error(gomock.Any(), gomock.Any()).AnyTimes()

	uc := useroom.NewDeleteUseCase(registry, reservation, logger)
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

	uc := useroom.NewDeleteUseCase(registry, reservation, logger)
	err := uc.Delete(context.Background(), roomID, "bad")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}
