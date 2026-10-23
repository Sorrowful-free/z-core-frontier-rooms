//go:build enet && cgo

package enet

import (
	"context"
	"errors"
	"fmt"
	"time"

	transportenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	deliveryerrors "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/errors"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/enet"
	libenet "github.com/codecat/go-enet"
)

// Все вызовы libenet (Host/Peer/Packet) сериализует HostLoop владеющего host'а
// (adapter/transport/enet/hostloop_cgo.go): host.Service идёт blocking-
// командой Call в loop, peer/packet-операции внутри dispatch - Call/Do в тот
// же loop. Сам dispatch исполняется на run-горутине, НЕ на loop-горутине:
// admit/leave путь (JoinRoom/LeaveRoom → dispatchLifecycle) синхронно ждёт
// room-горутину, а та в OnJoin/OnLeave делает EnetConnection.Ping - blocking
// Call в этот же loop. Dispatch на loop-горутине замыкал бы cross-deadlock
// executor↔room; на run-горутине блокировка безобидна, executor при этом
// свободен и обслуживает Ping/conn-операции.
// sessions и поля peerSession трогает только run-горутина - mutex не нужен.
//
// P18: run исполняется по одной горутине на host (порт), все host'ы делят
// общий ctx mux'а (Listen). Потолок одного host'а - PeerLimit и pps его
// HostLoop'а; capacity комнаты к host'у не привязана (room - транспорт-агностик).
func (h *RoomsHandler) run(ctx context.Context, host *enetHost) error {
	// Initialize идемпотентен (refcount в libenet): N run-горутин mux'а зовут
	// его без лока; Deinitialize снимает только свой refcount, реальный
	// teardown - после выхода всех loop'ов.
	libenet.Initialize()
	defer libenet.Deinitialize()

	loop, err := transportenet.NewHostLoop(
		libenet.NewListenAddress(host.port),
		h.cfg.PeerLimit,
		h.cfg.ChannelLimit,
		h.metrics,
	)
	if err != nil {
		return fmt.Errorf("enet host create port %d: %w", host.port, err)
	}
	// Stop идемпотентен и ждёт выхода executor'а (drain + host.Destroy);
	// до Deinitialize - обратный порядок.
	defer loop.Stop()
	// Peer→host привязка: у каждого host'а СВОЯ фабрика (WithLoop клонирует
	// инжектированную - shared factory c BindLoop на N host'ах гоняла бы
	// conn'ы чужого loop'а).
	connFactory := h.connectionFactory
	if f, ok := h.connectionFactory.(*transportenet.EnetConnectionFactory); ok {
		connFactory = f.WithLoop(loop)
	}

	h.logger.Info("enet host listening", "port", host.port, "hostID", host.id)

	sessions := make(map[uint16]*peerSession)

	for {
		if err := ctx.Err(); err != nil {
			h.closeSessions(sessions)
			// Best-effort drain: conn.Close из closeSessions закинул graceful
			// DisconnectLater в очередь loop'а (FIFO - исполнится до service-
			// команд ниже); пара service-итераций выгонит их на провод до Destroy.
			h.drainHost(loop)
			return err
		}

		h.disconnectExpiredAdmits(sessions)

		ev, err := transportenet.Call(loop, func() libenet.Event {
			return loop.Host().Service(h.cfg.ServiceTimeoutMs)
		})
		if err != nil {
			// Loop остановлен извне: shutdown-последовательность выше уже
			// отработала либо не нужна (host на пути к Destroy).
			h.closeSessions(sessions)
			return transportenet.ErrHostLoopStopped
		}
		h.dispatchEvent(ctx, host, loop, connFactory, sessions, ev)
	}
}

// dispatchEvent - обработка одного события Service на run-горутине; обращения
// к peer/packet - через Call/Do в loop (Event-аксессоры GetType/GetPeer/
// GetPacket/GetData читают Go-копию события и диспатча не требуют).
func (h *RoomsHandler) dispatchEvent(
	ctx context.Context,
	host *enetHost,
	loop *transportenet.HostLoop,
	connFactory enet.EnetConnectionFactory,
	sessions map[uint16]*peerSession,
	ev libenet.Event,
) {
	switch ev.GetType() {
	case libenet.EventNone:
		return

	case libenet.EventConnect:
		sess := newPeerSession(host, loop, ev.GetPeer(), connFactory, h.cfg.EnetIncomingQueueSize, h.metrics)
		sessions[sess.id] = sess
		h.logger.Info("enet peer connected", "enet_peer_id", sess.id, "hostID", host.id)

	case libenet.EventReceive:
		id, _ := transportenet.Call(loop, func() uint16 { return ev.GetPeer().GetIncomingPeerId() })
		packet := ev.GetPacket()

		sess, ok := sessions[id]
		if !ok {
			_ = loop.Do(func() error { packet.Destroy(); return nil })
			h.logger.Warn("enet receive from unknown peer", "enet_peer_id", id)
			return
		}

		if !sess.admitted {
			data, _ := transportenet.Call(loop, func() []byte { return packet.GetData() })
			_ = loop.Do(func() error { packet.Destroy(); return nil })
			if h.packetTooLarge(len(data)) {
				h.logger.Warn("enet admit packet too large", "enet_peer_id", id, "bytes", len(data))
				h.disconnectSession(ctx, sessions, id, sess)
				return
			}
			if err := h.admit(ctx, host, sess, data); err != nil {
				// Счётчик считает отправленные reject-кадры, не попытки
				// (P19b): эмит только при успешном send, ошибка - в лог.
				if sendErr := deliveryerrors.SendJoinReject(sess.conn, err); sendErr != nil {
					h.logger.Warn("enet join reject send failed", "error", sendErr, "enet_peer_id", id)
				} else {
					h.metrics.JoinRejected(deliveryerrors.JoinRejectOpCode(err))
				}
				// Graceful close: conn.Close кладёт DisconnectLater в очередь
				// loop'а - сначала на провод уходит queued reliable reject
				// (тот же FIFO), затем disconnect (P7: reject доезжает).
				_ = sess.close()
				delete(sessions, id)
			}
			return
		}

		frame, err := frameFromPacket(loop, packet, h.cfg.MaxIncomingFrameBytes)
		_ = loop.Do(func() error { packet.Destroy(); return nil })
		if err != nil {
			if errors.Is(err, domain.ErrIncomingFrameTooLarge) {
				h.logger.Warn("enet frame too large", "enet_peer_id", id)
			} else {
				h.logger.Warn("enet invalid frame", "error", err, "enet_peer_id", id)
			}
			h.disconnectSession(ctx, sessions, id, sess)
			return
		}
		if !sess.deliver(frame) {
			h.logger.Warn("enet incoming queue full, dropping frame", "enet_peer_id", id)
			h.metrics.EnetIncomingDropped()
		}

	case libenet.EventDisconnect:
		id, _ := transportenet.Call(loop, func() uint16 { return ev.GetPeer().GetIncomingPeerId() })
		sess, ok := sessions[id]
		delete(sessions, id)
		if ok {
			if sess.admitted && sess.roomID.IsValid() && sess.peerID.IsValid() {
				if err := h.leaveRoomUseCase.LeaveRoom(ctx, sess.roomID, sess.peerID); err != nil {
					h.logger.Error("enet leave room failed", "error", err, "enet_peer_id", id, "roomID", sess.roomID, "peerID", sess.peerID)
				}
			}
			_ = sess.close()
			h.logger.Info("enet peer disconnected", "enet_peer_id", id, "data", ev.GetData())
		}
	}
}

// admit - первый пакет peer'а. Strict binding (P18): claims.HostID ticket'а
// обязан совпасть с host'ом, принявшим соединение, иначе join-reject
// (ErrWrongHost → OpWrongHost 0x77). Ticket без hostID (старый v2 или
// выписанный без registry) проходит на любом host'е - backward compat.
// Проверка до JoinRoom: на чужом host'е ticket не должен тратить слот
// reservation и тем более join'ить peer'а.
func (h *RoomsHandler) admit(ctx context.Context, host *enetHost, sess *peerSession, token []byte) error {
	if len(token) == 0 {
		h.logger.Warn("enet connect: empty token", "enet_peer_id", sess.id)
		return domain.ErrEmptyToken
	}

	claims, err := h.admission.Validate(ctx, token)
	if err != nil {
		h.logger.Warn("enet admit: invalid ticket", "error", err, "enet_peer_id", sess.id)
		return err
	}
	if claims.HostID != domain.HostIDAny && claims.HostID != host.id {
		h.logger.Warn("enet admit: ticket bound to another host", "enet_peer_id", sess.id, "wantHostID", claims.HostID, "hostID", host.id)
		return domain.ErrWrongHost
	}

	summary, peerID, err := h.joinRoomUseCase.JoinRoom(ctx, sess.conn, token)
	if err != nil {
		h.logger.Error("enet join room failed", "error", err, "enet_peer_id", sess.id, "op", deliveryerrors.JoinRejectOpCode(err))
		return err
	}

	sess.admitted = true
	sess.roomID = summary.ID
	sess.peerID = peerID
	h.logger.Info("enet peer admitted", "enet_peer_id", sess.id, "hostID", host.id)
	return nil
}

func (h *RoomsHandler) packetTooLarge(size int) bool {
	max := h.cfg.MaxIncomingFrameBytes
	return max > 0 && size > max
}

// disconnectSession - LeaveRoom (если admitted) + graceful close + удаление из map.
// conn.Close кладёт graceful peer.Disconnect в очередь loop'а: queued outbound
// пакеты уходят до разрыва (FIFO сохраняет порядок reject → disconnect).
func (h *RoomsHandler) disconnectSession(
	ctx context.Context,
	sessions map[uint16]*peerSession,
	id uint16,
	sess *peerSession,
) {
	if sess.admitted && sess.roomID.IsValid() && sess.peerID.IsValid() {
		if err := h.leaveRoomUseCase.LeaveRoom(ctx, sess.roomID, sess.peerID); err != nil {
			h.logger.Error("enet leave room failed", "error", err, "enet_peer_id", id, "roomID", sess.roomID, "peerID", sess.peerID)
		}
	}
	_ = sess.close()
	delete(sessions, id)
}

// disconnectExpiredAdmits отключает peers, занявших PeerLimit-слот на EventConnect,
// но не прошедших admit за cfg.AdmitTimeout. Admitted сессии не трогает -
// их чистит idle peer sweep.
func (h *RoomsHandler) disconnectExpiredAdmits(sessions map[uint16]*peerSession) {
	deadline := time.Now().Add(-h.cfg.AdmitTimeout)
	for id, sess := range sessions {
		if sess.admitted || sess.connectedAt.After(deadline) {
			continue
		}
		h.logger.Warn("enet admit timeout, disconnecting peer", "enet_peer_id", id)
		_ = sess.close()
		delete(sessions, id)
	}
}

// closeSessions закрывает все сессии при shutdown: conn.Close закрывает
// closed-канал (будит Receive-горутины peer'ов) и кладёт graceful disconnect в
// очередь loop'а - drainHost после него выгонит их на провод. LeaveRoom/Revoke
// для admitted peers здесь не выполняется - их снимает registry shutdown
// (DeleteForShutdown) до вызова Stop; EventDisconnect, который drainHost
// проглатывает, leave-путь тоже не прогоняет.
func (h *RoomsHandler) closeSessions(sessions map[uint16]*peerSession) {
	for id, sess := range sessions {
		_ = sess.close()
		delete(sessions, id)
	}
}

// drainHost - best-effort: всего 2 service-итерации (~2xServiceTimeoutMs) через
// очередь loop'а перед Stop/Destroy. Peer с большим backlog может не успеть
// отправить очередь и получит hard disconnect вместо graceful - приемлемо:
// shutdown-путь уже сигналит о завершении процесса, и ждать произвольно долго
// нельзя. Ошибки Call (loop уже остановлен) игнорируем - путь best-effort.
func (h *RoomsHandler) drainHost(loop *transportenet.HostLoop) {
	for range 2 {
		_, _ = transportenet.Call(loop, func() libenet.Event {
			return loop.Host().Service(h.cfg.ServiceTimeoutMs)
		})
	}
}
