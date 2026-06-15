package room

import (
	"context"
	"errors"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
)

type DeleteUseCase struct {
	roomRegistry registry.RoomRegistry
	reservation  reservation.Reservation
	leaveRoom    *LeaveRoomUseCase
	logging      logging.Logger
}

func NewDeleteUseCase(
	roomRegistry registry.RoomRegistry,
	reservation reservation.Reservation,
	leaveRoom *LeaveRoomUseCase,
	logging logging.Logger,
) *DeleteUseCase {
	return &DeleteUseCase{
		roomRegistry: roomRegistry,
		reservation:  reservation,
		leaveRoom:    leaveRoom,
		logging:      logging,
	}
}

func (uc *DeleteUseCase) Delete(ctx context.Context, roomID domain.RoomID, password string) error {

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := uc.reservation.VerifyRoomPassword(ctx, roomID, password); err != nil {
		uc.logging.Error("delete room: invalid password", "error", err, "roomID", roomID)
		return err
	}

	return uc.deleteRoom(ctx, roomID)
}

// DeleteForShutdown удаляет комнату без проверки пароля (graceful shutdown процесса).
func (uc *DeleteUseCase) DeleteForShutdown(ctx context.Context, roomID domain.RoomID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return uc.deleteRoom(ctx, roomID)
}

func (uc *DeleteUseCase) deleteRoom(ctx context.Context, roomID domain.RoomID) error {
	room, err := uc.roomRegistry.GetRoom(ctx, roomID)
	if err != nil {
		uc.logging.Error("delete room: get room failed", "error", err, "roomID", roomID)
		return err
	}

	kickErr := uc.kickAllPeers(ctx, room)
	if kickErr != nil {
		uc.logging.Warn("delete room: kick peers had errors", "error", kickErr, "roomID", roomID)
	}

	if err := uc.roomRegistry.DeleteRoom(ctx, roomID); err != nil {
		uc.logging.Error("delete room: delete room failed", "error", err, "roomID", roomID)
		return errors.Join(kickErr, err)
	}

	if err := uc.reservation.UnregisterRoom(ctx, roomID); err != nil {
		uc.logging.Error("delete room: unregister room failed", "error", err, "roomID", roomID)
		return errors.Join(kickErr, err)
	}

	uc.logging.Info("delete room: delete room success", "roomID", roomID)
	return nil
}

func (uc *DeleteUseCase) kickAllPeers(ctx context.Context, room realtime.Room) error {
	roomID := room.GetID()
	peers := room.GetPeers()

	var kickErr error
	for _, peer := range peers {
		peerID := peer.GetID()
		if err := uc.leaveRoom.LeaveRoom(ctx, roomID, peerID); err != nil {
			uc.logging.Error("delete room: kick peer failed", "error", err, "roomID", roomID, "peerID", peerID)
			kickErr = errors.Join(kickErr, err)
		}
	}
	return kickErr
}
