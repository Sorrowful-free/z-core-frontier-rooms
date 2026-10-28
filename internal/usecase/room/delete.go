package room

import (
	"context"
	"errors"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
)

type DeleteUseCase struct {
	roomRegistry registry.RoomRegistry
	reservation  reservation.Reservation
	logging      logging.Logger
}

func NewDeleteUseCase(
	roomRegistry registry.RoomRegistry,
	reservation reservation.Reservation,
	logging logging.Logger,
) *DeleteUseCase {
	return &DeleteUseCase{
		roomRegistry: roomRegistry,
		reservation:  reservation,
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

// DeleteForShutdown deletes a room without a password check (process graceful shutdown).
func (uc *DeleteUseCase) DeleteForShutdown(ctx context.Context, roomID domain.RoomID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return uc.deleteRoom(ctx, roomID)
}

// deleteRoom — the same teardown as policy-death (P29/P31): OpRoomClosed
// (0x82) to all peers → DeleteRoom → UnregisterRoom → parallel
// drain-aware Stop. Replaces the serial LeaveRoom loop: the very first
// master-kick on a no-migration room triggered closeRoom and the
// remaining LeaveRoom calls replied with ErrRoomNotFound (latent bug),
// plus O(N×drainTimeout).
// ErrRoomNotFound from DeleteRoom inside closeRoom is tolerated (Warn) —
// a race with a parallel room death.
func (uc *DeleteUseCase) deleteRoom(ctx context.Context, roomID domain.RoomID) error {
	room, err := uc.roomRegistry.GetRoom(ctx, roomID)
	if err != nil {
		uc.logging.Error("delete room: get room failed", "error", err, "roomID", roomID)
		return err
	}

	if err := closeRoom(ctx, room, uc.roomRegistry, uc.reservation, uc.logging); err != nil {
		// The DELETE contract is "room deleted": per-peer
		// deliver/stop errors joined inside closeRoom must not
		// turn into a 5xx when the goal is already achieved. We check
		// the fact: room absent from registry → Warn + success; present →
		// the error is real, return it.
		if _, getErr := uc.roomRegistry.GetRoom(ctx, roomID); errors.Is(getErr, domain.ErrRoomNotFound) {
			uc.logging.Warn("delete room: room deleted with teardown errors", "error", err, "roomID", roomID)
			return nil
		}
		return err
	}
	uc.logging.Info("delete room: delete room success", "roomID", roomID)
	return nil
}
