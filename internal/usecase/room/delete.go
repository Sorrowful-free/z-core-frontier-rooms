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

// DeleteForShutdown удаляет комнату без проверки пароля (graceful shutdown процесса).
func (uc *DeleteUseCase) DeleteForShutdown(ctx context.Context, roomID domain.RoomID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return uc.deleteRoom(ctx, roomID)
}

// deleteRoom — тот же teardown, что у policy-death (P29/P31): OpRoomClosed
// (0x82) всем peer'ам → DeleteRoom → UnregisterRoom → параллельные
// drain-aware Stop. Заменяет серийный LeaveRoom-цикл: первый же master-kick
// на authoritative-host комнате триггерил closeRoom и остальные LeaveRoom
// сыпали ErrRoomNotFound в ответ (латентный баг), плюс O(N×drainTimeout).
// ErrRoomNotFound из DeleteRoom внутри closeRoom tolerated (Warn) — гонка
// с параллельной смертью комнаты.
func (uc *DeleteUseCase) deleteRoom(ctx context.Context, roomID domain.RoomID) error {
	room, err := uc.roomRegistry.GetRoom(ctx, roomID)
	if err != nil {
		uc.logging.Error("delete room: get room failed", "error", err, "roomID", roomID)
		return err
	}

	if err := closeRoom(ctx, room, uc.roomRegistry, uc.reservation, uc.logging); err != nil {
		// Контракт DELETE — «комната удалена»: ошибки per-peer
		// deliver/stop, попавшие в join closeRoom'а, не должны
		// превращаться в 5xx, когда цель уже достигнута. Проверяем
		// факт: комнаты нет в registry → Warn + успех; есть →
		// ошибка реальная, отдаём её.
		if _, getErr := uc.roomRegistry.GetRoom(ctx, roomID); errors.Is(getErr, domain.ErrRoomNotFound) {
			uc.logging.Warn("delete room: room deleted with teardown errors", "error", err, "roomID", roomID)
			return nil
		}
		return err
	}
	uc.logging.Info("delete room: delete room success", "roomID", roomID)
	return nil
}
