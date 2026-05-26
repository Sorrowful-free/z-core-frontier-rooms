package room

import (
	"context"
	"errors"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
)

type CreateUseCase struct {
	roomRegistry registry.RoomRegistry
	reservation  reservation.Reservation
	logging      logging.Logger
}

func NewCreateUseCase(roomRegistry registry.RoomRegistry, reservation reservation.Reservation, logging logging.Logger) *CreateUseCase {
	return &CreateUseCase{
		roomRegistry: roomRegistry,
		reservation:  reservation,
		logging:      logging,
	}
}

func (uc *CreateUseCase) Create(ctx context.Context, roomID domain.RoomID, capacity int) (RoomSummary, error) {

	if err := ctx.Err(); err != nil {
		return EmptyRoomSummary, err
	}

	if err := uc.reservation.RegisterRoom(ctx, roomID, capacity); err != nil {
		uc.logging.Error("create room: register room failed", "error", err, "roomID", roomID, "capacity", capacity)
		return EmptyRoomSummary, err
	}

	room, err := uc.roomRegistry.CreateRoom(ctx, roomID, capacity)
	if err != nil {
		createErr := err
		if unregisterErr := uc.reservation.UnregisterRoom(ctx, roomID); unregisterErr != nil {
			uc.logging.Error("create room: unregister room failed", "error", unregisterErr, "roomID", roomID)
			return EmptyRoomSummary, errors.Join(createErr, unregisterErr)
		}
		uc.logging.Error("create room: create room failed", "error", createErr, "roomID", roomID, "capacity", capacity)
		return EmptyRoomSummary, createErr
	}

	uc.logging.Info("create room: create room success", "roomID", roomID, "capacity", capacity)
	return *NewRoomSummaryFromRoom(room), nil
}
