package room

import (
	"context"
	"errors"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

type JoinRoomUseCase struct {
	peerFactory  realtime.PeerFactory
	roomRegistry registry.RoomRegistry
	reservation  reservation.Reservation
	logging      logging.Logger
}

func NewJoinRoomUseCase(
	peerFactory realtime.PeerFactory,
	roomRegistry registry.RoomRegistry,
	reservation reservation.Reservation,
	logging logging.Logger,
) *JoinRoomUseCase {
	return &JoinRoomUseCase{
		peerFactory:  peerFactory,
		roomRegistry: roomRegistry,
		reservation:  reservation,
		logging:      logging,
	}
}

// JoinRoom — the join flow behind AdmitPeerUseCase (P20, T2): claims are
// already validated by the facade (HMAC checked once at the entry — no
// second check).
func (uc *JoinRoomUseCase) JoinRoom(ctx context.Context, connection transport.Connection, claims domain.Claims) (PeerSession, error) {

	if err := ctx.Err(); err != nil {
		return PeerSession{}, err
	}

	room, err := uc.roomRegistry.GetRoom(ctx, claims.RoomID)
	if err != nil {
		return PeerSession{}, err
	}

	if err := uc.reservation.Admit(ctx, claims.RoomID, claims.PeerID); err != nil {
		uc.logging.Error("join room: admit failed", "error", err, "roomID", claims.RoomID, "peerID", claims.PeerID)
		return PeerSession{}, err
	}

	session, err := placePeerJoin(uc.peerFactory, room, connection, claims.PeerID, claims.NickName, uc.logging)
	if err != nil {
		return PeerSession{}, uc.revokeJoinReservation(ctx, room, claims.RoomID, claims.PeerID, err)
	}

	uc.logging.Info("join room: join room success", "roomID", claims.RoomID, "peerID", claims.PeerID)
	return session, nil
}

// revokeJoinReservation rolls back the slot after a failed join.
// On a Replace error the old peer stays in the room — Revoke is not called.
// A Revoke error is joined with joinErr and returned to the caller.
func (uc *JoinRoomUseCase) revokeJoinReservation(
	ctx context.Context,
	room realtime.Room,
	roomID domain.RoomID,
	peerID domain.PeerID,
	joinErr error,
) error {
	if joinErr == nil {
		return nil
	}
	if room.HasPeer(peerID) {
		return joinErr
	}
	if revokeErr := uc.reservation.Revoke(ctx, roomID, peerID); revokeErr != nil {
		uc.logging.Error("join room: revoke reservation failed", "error", revokeErr, "roomID", roomID, "peerID", peerID)
		return errors.Join(joinErr, revokeErr)
	}
	return joinErr
}
