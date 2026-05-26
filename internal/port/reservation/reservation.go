package reservation

import (
	"context"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type Reservation interface {
	RegisterRoom(ctx context.Context, roomID domain.RoomID, capacity int, password string) error
	UnregisterRoom(ctx context.Context, roomID domain.RoomID) error
	VerifyRoomPassword(ctx context.Context, roomID domain.RoomID, password string) error

	Reserve(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, expiresAt time.Time) error
	Admit(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) error
	Revoke(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) error

	State(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) (domain.ReservationSlot, error)
}
