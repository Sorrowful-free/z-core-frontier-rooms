package realtime

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type RoomFactory interface {
	CreateRoom(ctx context.Context, id domain.RoomID, capacity int) (Room, error)
}
