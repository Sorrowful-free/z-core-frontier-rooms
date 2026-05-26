package room

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
)

type DeleteUseCase struct {
	roomRegistry registry.RoomRegistry
	logging      logging.Logger
}

func NewDeleteUseCase(roomRegistry registry.RoomRegistry, logging logging.Logger) *DeleteUseCase {
	return &DeleteUseCase{
		roomRegistry: roomRegistry,
		logging:      logging,
	}
}

func (uc *DeleteUseCase) Delete(ctx context.Context, roomID domain.RoomID) error {

	if err := ctx.Err(); err != nil {
		return err
	}

	return uc.roomRegistry.DeleteRoom(ctx, roomID)
}
