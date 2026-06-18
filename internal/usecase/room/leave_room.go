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

	if err := room.Leave(peer); err != nil {
		if !room.HasPeer(peerID) {
			uc.logging.Warn("leave room: peer already left", "roomID", roomID, "peerID", peerID)
			return uc.revokeLeaveReservation(ctx, roomID, peerID)
		}
		uc.logging.Error("leave room: room leave failed", "error", err, "roomID", roomID, "peerID", peerID)
		return err
	}

	if err := peer.Stop(); err != nil {
		uc.logging.Error("leave room: peer stop failed", "error", err, "roomID", roomID, "peerID", peerID)
		return err
	}

	if err := uc.revokeLeaveReservation(ctx, roomID, peerID); err != nil {
		uc.logging.Error("leave room: reservation revoke failed", "error", err, "roomID", roomID, "peerID", peerID)
		return err
	}

	uc.logging.Info("peer left room", "roomID", roomID, "peerID", peerID)
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
