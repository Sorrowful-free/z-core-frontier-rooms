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

	// MarkDisconnected — start of reconnect grace (P20): the admitted slot
	// is not released, lastSeen is updated to the disconnect moment.
	MarkDisconnected(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) error
	// SlotDetails — read-only view of an admitted slot (P24): returns the
	// slot WITHOUT refreshing lastSeen (unlike Reconnect) — the source of
	// an honest timestamp for the grace deadline check. No slot or not
	// admitted → ErrReservationNotReserved.
	SlotDetails(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) (domain.AdmittedSlot, error)
	// Reconnect — atomic admitted slot check + lastSeen refresh + slot
	// return (NickName is the nickname source on resume). No slot or not
	// admitted → ErrReservationNotReserved.
	Reconnect(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) (domain.AdmittedSlot, error)

	State(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) (domain.ReservationSlot, error)
	ListAdmittedPeers(ctx context.Context, roomID domain.RoomID) ([]domain.AdmittedSlot, error)
	// ListRoomSlots — read-only listing of all room slots regardless of
	// state (P31): needed by the never-joined rooms sweep to check for
	// "neither admitted nor unexpired reserve". No room / tombstone →
	// ErrReservationNotFound (equivalent to "no slots" for the sweep).
	ListRoomSlots(ctx context.Context, roomID domain.RoomID) ([]domain.RoomSlotDetail, error)
}
