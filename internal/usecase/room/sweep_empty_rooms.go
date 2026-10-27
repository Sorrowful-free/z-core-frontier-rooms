package room

import (
	"context"
	"errors"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
)

// Значение label `sweep` для SweepRan (bounded в адаптере).
const sweepKindEmptyRooms = "empty_rooms"

// SweepEmptyRoomsUseCase — смерть never-joined комнат (P31): комната создана
// (POST /rooms + host-тикет), но никто не вошёл — reserve-слот чистится
// только лениво при следующем Reserve, а сама комната висела бы в registry
// вечно. Transition-проверки (closeRoomIfEmpty на уходе peer'а) этот класс
// не накрывают — peer'ов никогда не было, нужен time-based sweep.
//
// Предикат kill: peers == 0 ∧ ни одного admitted-слота ∧ все reserve-слоты
// истекли (expiresAt < now) или слотов нет. Живой reserve (unexpired
// ticket-claim) комнату держит — createdAt у комнаты нет и не нужен:
// дедлайн самого свежего тикета и есть порог. Zero ExpiresAt не истекает
// (тот же контракт, что у sweepExpiredLocked в adapter/reservation).
//
// Kill идёт через closeRoomIfEmpty: повторная проверка admitted перед
// DeleteRoom сужает окно гонки с параллельным admit'ом (admit между
// листингом слотов и kill вернёт len>0 → no-op). Свежий Reserve между
// листингом и kill проверка не ловит — joiner получит reject вместо
// комнаты (accepted-гонка, симметрично transition-пути).
type SweepEmptyRoomsUseCase struct {
	roomRegistry registry.RoomRegistry
	reservation  reservation.Reservation
	logger       logging.Logger
	metrics      portmetrics.Metrics
}

func NewSweepEmptyRoomsUseCase(
	roomRegistry registry.RoomRegistry,
	reservation reservation.Reservation,
	logger logging.Logger,
	m portmetrics.Metrics,
) *SweepEmptyRoomsUseCase {
	return &SweepEmptyRoomsUseCase{
		roomRegistry: roomRegistry,
		reservation:  reservation,
		logger:       logger,
		metrics:      portmetrics.OrNoop(m),
	}
}

// RunOnce проходит по комнатам и убивает пустые без живых слотов.
// Возвращает число убитых комнат за проход.
func (uc *SweepEmptyRoomsUseCase) RunOnce(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// Проход sweep считается отдельно от обработанных записей (P19b).
	uc.metrics.SweepRan(sweepKindEmptyRooms)

	rooms, err := uc.roomRegistry.GetList(ctx)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	killed := 0

	for _, room := range rooms {
		if err := ctx.Err(); err != nil {
			return killed, err
		}
		if len(room.GetPeers()) > 0 {
			continue
		}
		roomID := room.GetID()

		slots, err := uc.reservation.ListRoomSlots(ctx, roomID)
		if err != nil {
			// Комната уже unregister'нута из reservation параллельным
			// teardown — слотов нет, предикат kill выполнен. Прочие
			// ошибки — состояние неизвестно, пропуск до следующего прохода.
			if !errors.Is(err, domain.ErrReservationNotFound) {
				uc.logger.Error("empty rooms sweep: list slots failed", "error", err, "roomID", roomID)
				continue
			}
			slots = nil
		}

		if hasLiveReservationSlot(slots, now) {
			continue
		}

		uc.logger.Warn("empty rooms sweep: room has no live slots", "roomID", roomID)
		if closeRoomIfEmpty(ctx, room, uc.roomRegistry, uc.reservation, uc.logger) {
			killed++
		}
	}

	return killed, nil
}

// hasLiveReservationSlot — true, если хоть один слот ещё удерживает комнату:
// admitted (живой или в grace — reconnect ещё возможен) либо reserve с
// неистёкшим дедлайном тикета. ExpiresAt.IsZero() не истекает, как в
// sweepExpiredLocked adapter'а.
func hasLiveReservationSlot(slots []domain.RoomSlotDetail, now time.Time) bool {
	for _, slot := range slots {
		switch slot.State {
		case domain.ReservationSlotAdmitted:
			return true
		case domain.ReservationSlotReserved:
			if slot.ExpiresAt.IsZero() || !slot.ExpiresAt.Before(now) {
				return true
			}
		}
	}
	return false
}

// StartEmptyRoomsSweep запускает фоновый sweep; interval <= 0 — no-op (nil
// handle). Каденс — тот же RESERVATION_ORPHAN_SWEEP_INTERVAL, что у
// orphan-sweep: 0 отключает оба.
func StartEmptyRoomsSweep(
	ctx context.Context,
	uc *SweepEmptyRoomsUseCase,
	interval time.Duration,
	logger logging.Logger,
) *SweepHandle {
	return startSweep(ctx, interval, logger, "empty rooms sweep", "killed", uc.RunOnce)
}
