package identity

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type Allocator interface {
	AllocateRoomID(ctx context.Context) (domain.RoomID, error)
	AllocatePeerID(ctx context.Context) (domain.PeerID, error)
}
