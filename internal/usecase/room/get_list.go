package room

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
)

type GetListUseCase struct {
	roomRegistry registry.RoomRegistry
	logging      logging.Logger
}

func NewGetListUseCase(roomRegistry registry.RoomRegistry, logging logging.Logger) *GetListUseCase {
	return &GetListUseCase{
		roomRegistry: roomRegistry,
		logging:      logging,
	}
}

func (uc *GetListUseCase) GetList(ctx context.Context) ([]RoomSummary, error) {

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rooms, err := uc.roomRegistry.GetList(ctx)
	if err != nil {
		return nil, err
	}
	roomSummaries := make([]RoomSummary, len(rooms))
	for i, room := range rooms {
		roomSummaries[i] = *NewRoomSummaryFromRoom(room)
	}
	return roomSummaries, nil
}
