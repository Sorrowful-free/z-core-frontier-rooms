package room

import (
	"context"

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
		if err := uc.reservation.UnregisterRoom(ctx, roomID); err != nil {
			uc.logging.Error("create room: unregister room failed", "error", err, "roomID", roomID)
			return EmptyRoomSummary, err
		}
		uc.logging.Error("create room: create room failed", "error", err, "roomID", roomID, "capacity", capacity)
		return EmptyRoomSummary, err
	}
	uc.logging.Info("create room: create room success", "roomID", roomID, "capacity", capacity)
	return *NewRoomSummaryFromRoom(room), nil
}
