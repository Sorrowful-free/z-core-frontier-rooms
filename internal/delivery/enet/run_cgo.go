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
	libenet "github.com/codecat/go-enet"
)

// Все вызовы libenet (Host/Peer/Packet) идут через transportenet.Call/Do —
// единый mutex сериализации C API, см. adapter/transport/enet/connection_cgo.go.
// sessions и поля peerSession трогает только host-loop горутина — отдельный mutex не нужен.
func (h *RoomsHandler) run(ctx context.Context) error {
	transportenet.Do(libenet.Initialize)
	defer transportenet.Do(libenet.Deinitialize)

	var host libenet.Host
	err := transportenet.Call(func() error {
		var err error
		host, err = libenet.NewHost(
			libenet.NewListenAddress(h.cfg.ListenPort),
			h.cfg.PeerLimit,
			h.cfg.ChannelLimit,
			0,
			0,
		)
		return err
	})
	if err != nil {
		return fmt.Errorf("enet host create port %d: %w", h.cfg.ListenPort, err)
	}
	defer transportenet.Do(func() { host.Destroy() })

	h.logger.Info("enet host listening", "port", h.cfg.ListenPort)

	sessions := make(map[uint16]*peerSession)

	for {
		if err := ctx.Err(); err != nil {
			h.closeSessions(sessions)
			// Best-effort drain: пара service-итераций, чтобы queued graceful
			// disconnect'ы ушли на провод до host.Destroy.
			h.drainHost(host)
			return err
		}

		h.disconnectExpiredAdmits(sessions)

		ev := transportenet.Call(func() libenet.Event {
			return host.Service(h.cfg.ServiceTimeoutMs)
		})
		switch ev.GetType() {
		case libenet.EventNone:
			continue

		case libenet.EventConnect:
			sess := newPeerSession(ev.GetPeer(), h.connectionFactory, h.cfg.EnetIncomingQueueSize)
			sessions[sess.id] = sess
			h.logger.Info("enet peer connected", "enet_peer_id", sess.id)

		case libenet.EventReceive:
			id := transportenet.Call(func() uint16 { return ev.GetPeer().GetIncomingPeerId() })
			packet := ev.GetPacket()

			sess, ok := sessions[id]
			if !ok {
				transportenet.Do(packet.Destroy)
				h.logger.Warn("enet receive from unknown peer", "enet_peer_id", id)
				continue
			}

			if !sess.admitted {
				data := transportenet.Call(func() []byte { return packet.GetData() })
				transportenet.Do(packet.Destroy)
				if h.packetTooLarge(len(data)) {
					h.logger.Warn("enet admit packet too large", "enet_peer_id", id, "bytes", len(data))
					h.disconnectSession(ctx, sessions, id, sess)
					continue
				}
				if err := h.admit(ctx, sess, data); err != nil {
					deliveryerrors.SendJoinReject(sess.conn, err)
					// Graceful close: conn.Close → peer.Disconnect — сначала на провод
					// уходит queued reliable reject, затем disconnect (P7: reject доезжает).
					_ = sess.close()
					delete(sessions, id)
				}
				continue
			}

			frame, err := frameFromPacket(packet, h.cfg.MaxIncomingFrameBytes)
			transportenet.Do(packet.Destroy)
			if err != nil {
				if errors.Is(err, domain.ErrIncomingFrameTooLarge) {
					h.logger.Warn("enet frame too large", "enet_peer_id", id)
				} else {
					h.logger.Warn("enet invalid frame", "error", err, "enet_peer_id", id)
				}
				h.disconnectSession(ctx, sessions, id, sess)
				continue
			}
			if !sess.deliver(frame) {
				h.logger.Warn("enet incoming queue full, dropping frame", "enet_peer_id", id)
			}

		case libenet.EventDisconnect:
			id := transportenet.Call(func() uint16 { return ev.GetPeer().GetIncomingPeerId() })
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
}

func (h *RoomsHandler) admit(ctx context.Context, sess *peerSession, token []byte) error {
	if len(token) == 0 {
		h.logger.Warn("enet connect: empty token", "enet_peer_id", sess.id)
		return domain.ErrEmptyToken
	}

	summary, peerID, err := h.joinRoomUseCase.JoinRoom(ctx, sess.conn, token)
	if err != nil {
		h.logger.Error("enet join room failed", "error", err, "enet_peer_id", sess.id, "op", deliveryerrors.JoinRejectOpCode(err))
		return err
	}

	sess.admitted = true
	sess.roomID = summary.ID
	sess.peerID = peerID
	h.logger.Info("enet peer admitted", "enet_peer_id", sess.id)
	return nil
}

func (h *RoomsHandler) packetTooLarge(size int) bool {
	max := h.cfg.MaxIncomingFrameBytes
	return max > 0 && size > max
}

// disconnectSession — LeaveRoom (если admitted) + graceful close + удаление из map.
// conn.Close кладёт graceful peer.Disconnect: queued outbound пакеты уходят до разрыва.
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
// но не прошедших admit за cfg.AdmitTimeout. Admitted сессии не трогает —
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
// closed-канал (будит Receive-горутины peer'ов) и шлёт graceful disconnect.
// LeaveRoom/Revoke для admitted peers здесь не выполняется — их снимает
// registry shutdown (DeleteForShutdown) до вызова Stop; EventDisconnect,
// который drainHost проглатывает, leave-путь тоже не прогоняет.
func (h *RoomsHandler) closeSessions(sessions map[uint16]*peerSession) {
	for id, sess := range sessions {
		_ = sess.close()
		delete(sessions, id)
	}
}

// drainHost — best-effort: всего 2 service-итерации (~2×ServiceTimeoutMs) перед
// host.Destroy. Peer с большим backlog может не успеть отправить очередь и
// получит hard disconnect вместо graceful — приемлемо: shutdown-путь уже
// сигналит о завершении процесса, и ждать произвольно долго нельзя.
func (h *RoomsHandler) drainHost(host libenet.Host) {
	for range 2 {
		transportenet.Call(func() libenet.Event {
			return host.Service(h.cfg.ServiceTimeoutMs)
		})
	}
}
