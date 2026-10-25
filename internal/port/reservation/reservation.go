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

	Reserve(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, nickName string, expiresAt time.Time) error
	Admit(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) error
	Revoke(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) error

	// MarkDisconnected — начало reconnect grace (P20): admitted-слот не
	// освобождается, admittedAt переиспользуется как lastSeen разрыва.
	MarkDisconnected(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) error
	// Reconnect — атомарная проверка admitted-слота + refresh lastSeen +
	// возврат слота (NickName — источник ника при resume). Слота нет или
	// он не admitted → ErrReservationNotReserved.
	Reconnect(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) (domain.AdmittedSlot, error)

	State(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) (domain.ReservationSlot, error)
	ListAdmittedPeers(ctx context.Context, roomID domain.RoomID) ([]domain.AdmittedSlot, error)
}
