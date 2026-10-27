package room

import (
	"context"
	"errors"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
)

// DisconnectPeerUseCase — разрыв соединения без потери идентичности (P20):
// peer изымается из комнаты обычным leave-путём (state-запись чистится,
// election мгновенный, remove-патч уходит остальным), но слот reservation
// не ревокается — MarkDisconnected ставит lastSeen, от которого sweep
// отсчитывает RESERVATION_RECONNECT_TTL grace-окна; остальным рассылается
// OpPeerDisconnected. Все disconnect-триггеры (WS waiter, ENet
// EventDisconnect/bad-frame, idle sweep) идут сюда; LeaveRoomUseCase
// остаётся для явных leave и rollback'ов.
type DisconnectPeerUseCase struct {
	roomRegistry registry.RoomRegistry
	reservation  reservation.Reservation
	logging      logging.Logger
}

func NewDisconnectPeerUseCase(
	roomRegistry registry.RoomRegistry,
	reservation reservation.Reservation,
	logging logging.Logger,
) *DisconnectPeerUseCase {
	return &DisconnectPeerUseCase{
		roomRegistry: roomRegistry,
		reservation:  reservation,
		logging:      logging,
	}
}

func (uc *DisconnectPeerUseCase) DisconnectPeer(ctx context.Context, session PeerSession) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	room, err := uc.roomRegistry.GetRoom(ctx, session.RoomID)
	if err != nil {
		if errors.Is(err, domain.ErrRoomNotFound) {
			uc.logging.Warn("disconnect peer: room already gone", "roomID", session.RoomID, "peerID", session.PeerID)
			return nil
		}
		return err
	}

	// Identity pre-check до MarkDisconnected: session.Peer может быть
	// старым объектом, вытесненным Replace/resume (stale teardown) — метка
	// lastSeen и peer_disconnects_total принадлежат живому peer'у, а не
	// ему. Узкое окно до Leave закрывает ErrPeerStale в lifecycle.
	current, err := room.GetPeer(session.PeerID)
	switch {
	case errors.Is(err, domain.ErrPeerNotFound):
		uc.logging.Debug("disconnect peer: stale teardown (peer gone)", "roomID", session.RoomID, "peerID", session.PeerID)
		return nil
	case err != nil:
		return err
	case current != session.Peer:
		uc.logging.Debug("disconnect peer: stale teardown (peer replaced)", "roomID", session.RoomID, "peerID", session.PeerID)
		return nil
	}

	// Mark до leave: пока peer в комнате, sweep игнорирует слот (HasPeer) —
	// метка lastSeen встаёт без окна «слот-сирота без timestamp». Ошибка
	// метки не блокирует изъятие peer'а из комнаты.
	// Known issue: два конкурентных teardown'а одной сессии (idle sweep ∥
	// WS waiter / ENet EventDisconnect) могут оба пройти identity pre-check
	// выше и оба доехать до метки — двойной mark (peer_disconnects_total
	// +2, lastSeen освежается дважды). Broadcast при этом остаётся
	// одиночным: проигравший room.Leave получает ErrPeerNotFound и выходит
	// раньше рассылки. Принято как benign-шум, дедупликация не добавляется.
	// Stale-mark — гонка того же класса: mark стоит между identity pre-check
	// и авторитетным Leave, поэтому при вытеснении peer'а Replace'ом в этом
	// окне метка ложится на слот уже нового peer'а (метрика +1, lastSeen
	// освежается), а Leave затем отвечает ErrPeerStale.
	if err := uc.reservation.MarkDisconnected(ctx, session.RoomID, session.PeerID); err != nil {
		uc.logging.Error("disconnect peer: mark disconnected failed", "error", err, "roomID", session.RoomID, "peerID", session.PeerID)
	}

	leaveErr := room.Leave(session.Peer)
	switch {
	case errors.Is(leaveErr, domain.ErrPeerStale):
		// Ожидаемая гонка (T8): старый conn проснулся после Replace —
		// идентичность у нового peer'а; ничего не удаляем и не рассылаем.
		uc.logging.Debug("disconnect peer: stale teardown", "roomID", session.RoomID, "peerID", session.PeerID)
		return nil
	case errors.Is(leaveErr, domain.ErrPeerNotFound):
		uc.logging.Warn("disconnect peer: peer already left", "roomID", session.RoomID, "peerID", session.PeerID)
		return nil
	case errors.Is(leaveErr, domain.ErrRoomShouldClose):
		// Authoritative-host (P29): разрыв master'а убивает комнату сразу —
		// grace-окно хосту не применяется; оставшимся уходит OpRoomClosed и
		// разрыв, слоты (включая только что помеченный) ревокает teardown.
		stopErr := session.Peer.Stop()
		if stopErr != nil {
			uc.logging.Error("disconnect peer: peer stop failed", "error", stopErr, "roomID", session.RoomID, "peerID", session.PeerID)
		}
		return errors.Join(stopErr, closeRoom(ctx, room, uc.roomRegistry, uc.reservation, uc.logging))
	case leaveErr != nil:
		uc.logging.Error("disconnect peer: room leave failed", "error", leaveErr, "roomID", session.RoomID, "peerID", session.PeerID)
	}

	stopErr := session.Peer.Stop()
	if stopErr != nil {
		uc.logging.Error("disconnect peer: peer stop failed", "error", stopErr, "roomID", session.RoomID, "peerID", session.PeerID)
	}

	// FIFO outbound каждого получателя держит порядок «remove-патч → opcode».
	sendErr := room.Send(events.PeerEvent{
		Frame: domain.NewPeerNotificationFrame(domain.OpPeerDisconnected, session.PeerID),
	})
	if sendErr != nil {
		uc.logging.Error("disconnect peer: broadcast disconnected failed", "error", sendErr, "roomID", session.RoomID, "peerID", session.PeerID)
	}

	if err := errors.Join(leaveErr, stopErr, sendErr); err != nil {
		return err
	}
	uc.logging.Info("peer disconnected to grace", "roomID", session.RoomID, "peerID", session.PeerID)
	return nil
}
