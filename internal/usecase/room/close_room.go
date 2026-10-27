package room

import (
	"context"
	"errors"
	"runtime"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
)

// closeRoom — teardown комнаты по запросу policy (ErrRoomShouldClose, P29,
// authoritative-host: ушёл master — комната умирает). Те же шаги, что у
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
//   - Stop последним: Deliver коммитит кадр в write-loop получателя ещё до
//     conn.Close (на ENet порядок гарантирован жёстко — DisconnectLater в
//     той же FIFO-очередь host loop'а; на WS Send и Close идут из разных
//     горутин, unregister-шаги дают write-loop'у окно на флеш).
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
	// Deliver коммитит кадр в select-case припаркованного write-loop'а
	// (handoff), но сам conn.Send выполняется на его горутине — conn.Close
	// из нашего пути (peer.Stop ниже, либо read-loop по «room stopped»)
	// может обогнать запись. Уступаем квант, чтобы разбуженный write-loop
	// успел записать кадр на сокет до teardown'а. На ENet порядок гарантирован
	// жёстко (DisconnectLater в той же FIFO-очереди host loop'а), так что это
	// best-effort для WS; drain-aware close — уровень peer'а, не usecase.
	runtime.Gosched()

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

	for _, peer := range peers {
		if err := peer.Stop(); err != nil {
			logger.Error("close room: peer stop failed", "error", err, "roomID", roomID, "peerID", peer.GetID())
			resultErr = errors.Join(resultErr, err)
		}
	}

	if resultErr != nil {
		return resultErr
	}
	logger.Info("room closed by policy", "roomID", roomID)
	return nil
}
