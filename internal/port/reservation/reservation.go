package reservation

import (
	"context"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type Reservation interface {
	RegisterRoom(ctx context.Context, roomID domain.RoomID, capacity int) error
	UnregisterRoom(ctx context.Context, roomID domain.RoomID) error

	Reserve(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, expiresAt time.Time) error
	Admit(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) error
	Revoke(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) error
}
