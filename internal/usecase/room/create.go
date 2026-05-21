package room

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
)

type CreateUseCase struct {
	roomRegistry registry.RoomRegistry
	logging      logging.Logger
}

func NewCreateUseCase(roomRegistry registry.RoomRegistry, logging logging.Logger) *CreateUseCase {
	return &CreateUseCase{
		roomRegistry: roomRegistry,
		logging:      logging,
	}
}

func (uc *CreateUseCase) Create(ctx context.Context, roomID domain.RoomID) (RoomSummary, error) {
	room, err := uc.roomRegistry.CreateRoom(ctx, roomID)
	if err != nil {
		return EmptyRoomSummary, err
	}
	return *NewRoomSummaryFromRoom(room), nil
}
