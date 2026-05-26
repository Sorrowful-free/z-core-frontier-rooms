package room

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
)

type DeleteUseCase struct {
	roomRegistry registry.RoomRegistry
	reservation  reservation.Reservation
	logging      logging.Logger
}

func NewDeleteUseCase(roomRegistry registry.RoomRegistry, reservation reservation.Reservation, logging logging.Logger) *DeleteUseCase {
	return &DeleteUseCase{
		roomRegistry: roomRegistry,
		reservation:  reservation,
		logging:      logging,
	}
}

func (uc *DeleteUseCase) Delete(ctx context.Context, roomID domain.RoomID, password string) error {

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := uc.reservation.VerifyRoomPassword(ctx, roomID, password); err != nil {
		uc.logging.Error("delete room: invalid password", "error", err, "roomID", roomID)
		return err
	}

	if err := uc.roomRegistry.DeleteRoom(ctx, roomID); err != nil {
		uc.logging.Error("delete room: delete room failed", "error", err, "roomID", roomID)
		return err
	}

	if err := uc.reservation.UnregisterRoom(ctx, roomID); err != nil {
		uc.logging.Error("delete room: unregister room failed", "error", err, "roomID", roomID)
		return err
	}

	uc.logging.Info("delete room: delete room success", "roomID", roomID)
	return nil
}

// DeleteForShutdown удаляет комнату без проверки пароля (graceful shutdown процесса).
func (uc *DeleteUseCase) DeleteForShutdown(ctx context.Context, roomID domain.RoomID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := uc.roomRegistry.DeleteRoom(ctx, roomID); err != nil {
		return err
	}
	return uc.reservation.UnregisterRoom(ctx, roomID)
}
