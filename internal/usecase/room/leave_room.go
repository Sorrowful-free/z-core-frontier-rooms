package room

import (
	"context"
	"errors"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
)

type LeaveRoomUseCase struct {
	roomRegistry registry.RoomRegistry
	reservation  reservation.Reservation
	logging      logging.Logger
}

func NewLeaveRoomUseCase(roomRegistry registry.RoomRegistry, reservation reservation.Reservation, logging logging.Logger) *LeaveRoomUseCase {
	return &LeaveRoomUseCase{
		roomRegistry: roomRegistry,
		reservation:  reservation,
		logging:      logging,
	}
}

func (uc *LeaveRoomUseCase) LeaveRoom(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) error {

	if err := ctx.Err(); err != nil {
		return err
	}

	room, err := uc.roomRegistry.GetRoom(ctx, roomID)
	if err != nil {
		if errors.Is(err, domain.ErrRoomNotFound) {
			uc.logging.Warn("leave room: room already gone", "roomID", roomID, "peerID", peerID)
			return uc.revokeLeaveReservation(ctx, roomID, peerID)
		}
		return err
	}

	if !room.HasPeer(peerID) {
		uc.logging.Warn("leave room: peer already left", "roomID", roomID, "peerID", peerID)
		return uc.revokeLeaveReservation(ctx, roomID, peerID)
	}

	peer, err := room.GetPeer(peerID)
	if err != nil {
		return err
	}

	// room.Leave removes the peer from the room even on a policy error —
	// Stop and Revoke always run, the leave error is not swallowed.
	leaveErr := room.Leave(peer)
	if errors.Is(leaveErr, domain.ErrPeerStale) {
		// Resume displaced the object before the leave (P20): the identity
		// and the slot belong to the new peer — neither Stop nor Revoke is
		// allowed.
		uc.logging.Debug("leave room: stale peer object", "roomID", roomID, "peerID", peerID)
		return nil
	}
	if errors.Is(leaveErr, domain.ErrRoomShouldClose) {
		// Authoritative host (P29): a departed master kills the room — the
		// teardown (OpRoomClosed to the rest → disconnect everyone →
		// unregister) also revokes the departed peer's slot, no separate
		// Revoke needed.
		stopErr := peer.Stop()
		if stopErr != nil {
			uc.logging.Error("leave room: peer stop failed", "error", stopErr, "roomID", roomID, "peerID", peerID)
		}
		return errors.Join(stopErr, closeRoom(ctx, room, uc.roomRegistry, uc.reservation, uc.logging))
	}
	if leaveErr != nil {
		uc.logging.Error("leave room: room leave failed", "error", leaveErr, "roomID", roomID, "peerID", peerID)
	}

	stopErr := peer.Stop()
	if stopErr != nil {
		uc.logging.Error("leave room: peer stop failed", "error", stopErr, "roomID", roomID, "peerID", peerID)
	}

	revokeErr := uc.revokeLeaveReservation(ctx, roomID, peerID)
	if revokeErr != nil {
		uc.logging.Error("leave room: reservation revoke failed", "error", revokeErr, "roomID", roomID, "peerID", peerID)
	}

	if err := errors.Join(leaveErr, stopErr, revokeErr); err != nil {
		return err
	}

	uc.logging.Info("peer left room", "roomID", roomID, "peerID", peerID)
	// P31: the last peer left with no grace slots — the room dies at once
	// instead of hanging as a corpse in the registry until DELETE/shutdown.
	closeRoomIfEmpty(ctx, room, uc.roomRegistry, uc.reservation, uc.logging)
	return nil
}

func (uc *LeaveRoomUseCase) revokeLeaveReservation(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) error {
	if err := uc.reservation.Revoke(ctx, roomID, peerID); err != nil {
		if errors.Is(err, domain.ErrReservationNotFound) {
			uc.logging.Warn("leave room: reservation already gone", "roomID", roomID, "peerID", peerID)
			return nil
		}
		return err
	}
	return nil
}
