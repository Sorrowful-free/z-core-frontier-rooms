package room

import (
	"context"
	"errors"
	"sync"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
)

// closeRoom — teardown комнаты по запросу policy (ErrRoomShouldClose, P33,
// allowMasterMigration=false: ушёл master — комната умирает). Те же шаги, что у
// DeleteRoom, плюс терминальный wire-кадр: всем оставшимся peer'ам шлётся
// OpRoomClosed (0x82, payload пустой), затем unregister из registry, revoke
// слотов reservation (UnregisterRoom покрывает и слот ушедшего master'а) и
// disconnect каждого peer'а.
//
// Порядок имеет значение:
//   - Deliver до DeleteRoom: peer создаётся на room.Context() — Stop комнаты
//     отменяет и peer ctx, после чего tryDeliver вернёт «peer stopped».
//   - DeleteRoom до peer.Stop: conn close триггерит transport-колбэк
//     (ws waiter / ENet EventDisconnect → DisconnectPeer); когда комната уже
//     снята с реестра, колбэк упирается в ErrRoomNotFound и не нагоняет
//     grace-mark / OpPeerDisconnected поверх умирающей комнаты.
//   - Stop последним: drain-aware peer.Stop (P30) гарантирует порядок
//     «Send закоммиченного кадра до conn.Close» — write-loop сливает
//     outbound в пределах drain-таймаута до закрытия соединения (на ENet
//     SendBytes и DisconnectLater в одной FIFO-очереди host loop'а,
//     порядок строгий де-юре; на WS — Send вызван до Close, досыл
//     буфера — ядро).
func closeRoom(
	ctx context.Context,
	room realtime.Room,
	roomRegistry registry.RoomRegistry,
	reservation reservation.Reservation,
	logger logging.Logger,
) error {
	roomID := room.GetID()
	peers := room.GetPeers()

	var resultErr error
	for _, peer := range peers {
		if err := peer.Deliver(events.PeerEvent{
			Frame: domain.Frame{OpCode: domain.OpRoomClosed, Delivery: domain.DeliveryReliable},
		}); err != nil {
			logger.Error("close room: deliver room-closed failed", "error", err, "roomID", roomID, "peerID", peer.GetID())
			resultErr = errors.Join(resultErr, err)
		}
	}
	// Окно на флеш кадра больше не нужно: drain-aware peer.Stop (P30)
	// вызывает conn.Close только после того, как write-loop слил
	// закоммиченный до Stop outbound (bounded по drain-таймауту) — порядок
	// «Send 0x82 до Close» гарантирован на уровне peer'а, не scheduling-приёмом.

	if err := roomRegistry.DeleteRoom(ctx, roomID); err != nil {
		if errors.Is(err, domain.ErrRoomNotFound) {
			// Гонка с параллельным teardown (DeleteRoom / второй death-путь):
			// комната уже снята с реестра — цель достигнута, не ошибка.
			logger.Warn("close room: room already unregistered", "roomID", roomID)
		} else {
			logger.Error("close room: delete room failed", "error", err, "roomID", roomID)
			resultErr = errors.Join(resultErr, err)
		}
	}
	if err := reservation.UnregisterRoom(ctx, roomID); err != nil {
		logger.Error("close room: unregister room failed", "error", err, "roomID", roomID)
		resultErr = errors.Join(resultErr, err)
	}

	// Stop'ы — параллельно: drain-aware Stop (P30) ждёт writeDone до
	// drain-таймаута на заблокированном Send; серийный цикл дал бы
	// O(N×timeout) teardown (capacity до 127).
	var stopWG sync.WaitGroup
	var stopMu sync.Mutex
	for _, peer := range peers {
		stopWG.Add(1)
		go func() {
			defer stopWG.Done()
			if err := peer.Stop(); err != nil {
				logger.Error("close room: peer stop failed", "error", err, "roomID", roomID, "peerID", peer.GetID())
				stopMu.Lock()
				resultErr = errors.Join(resultErr, err)
				stopMu.Unlock()
			}
		}()
	}
	stopWG.Wait()

	if resultErr != nil {
		return resultErr
	}
	logger.Info("room closed by policy", "roomID", roomID)
	return nil
}

// closeRoomIfEmpty — смерть «пустой» комнаты (P31): teardown после реального
// изъятия последнего peer'а. «Пустая» = ни живых peers в комнате, ни
// admitted-слотов reservation: grace-слот (MarkDisconnected) остаётся
// admitted и держит reconnect — комната живёт до истечения
// RESERVATION_RECONNECT_TTL, пока orphan sweep не отзовёт слот.
//
// Pending reserve-тикеты kill не блокируют (в отличие от предиката
// SweepEmptyRooms): комната умирает по факту ухода всех вошедших —
// опоздавший joiner получит reject (0x43 / reservation-ошибку), а не
// пустую комнату.
//
// Kill идемпотентен через DeleteRoom (ErrRoomNotFound — tolerated, как в
// closeRoom). Гонка «join попал в умирающую комнату» та же, что у DELETE:
// peer приземляется в мёртвую комнату и его ctx умирает с room.Stop —
// принято без дополнительных гарантий. Ошибки логируются и не
// пропагируются: смерть пустой комнаты не должна фейлить завершившийся
// leave/disconnect. Возвращает true, если teardown реально выполнен.
func closeRoomIfEmpty(
	ctx context.Context,
	room realtime.Room,
	roomRegistry registry.RoomRegistry,
	reservation reservation.Reservation,
	logger logging.Logger,
) bool {
	if len(room.GetPeers()) > 0 {
		return false
	}
	roomID := room.GetID()

	admitted, err := reservation.ListAdmittedPeers(ctx, roomID)
	if err != nil {
		// ErrReservationNotFound — комната уже unregister'нута из
		// reservation параллельным teardown: слотов нет, kill законен.
		// Любая другая ошибка — состояние неизвестно, kill откладывается
		// до следующего триггера/sweep'а.
		if !errors.Is(err, domain.ErrReservationNotFound) {
			logger.Error("empty room: list admitted peers failed", "error", err, "roomID", roomID)
			return false
		}
	}
	if len(admitted) > 0 {
		return false
	}

	killed := true
	if err := roomRegistry.DeleteRoom(ctx, roomID); err != nil {
		if errors.Is(err, domain.ErrRoomNotFound) {
			// Гонка с параллельной смертью (closeRoom, DELETE, sweep):
			// комната уже снята с реестра — цель достигнута.
			logger.Warn("empty room: room already unregistered", "roomID", roomID)
		} else {
			logger.Error("empty room: delete room failed", "error", err, "roomID", roomID)
			killed = false
		}
	}
	if err := reservation.UnregisterRoom(ctx, roomID); err != nil {
		if errors.Is(err, domain.ErrReservationNotFound) {
			// Та же идемпотентность, что у DeleteRoom: слоты уже
			// сняты параллельным teardown — цель достигнута.
			logger.Warn("empty room: reservation already unregistered", "roomID", roomID)
		} else {
			logger.Error("empty room: unregister room failed", "error", err, "roomID", roomID)
			killed = false
		}
	}
	if killed {
		logger.Info("room died empty", "roomID", roomID)
	}
	return killed
}
