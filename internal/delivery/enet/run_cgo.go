//go:build enet && cgo

package enet

import (
	"context"
	"errors"
	"sync"

	deliveryerrors "github.com/Sorrowful-free/z-core-frontier-rooms/internal/delivery/errors"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

func (h *RoomsHandler) run(ctx context.Context) error {
	libenet.Initialize()
	defer libenet.Deinitialize()

	host, err := libenet.NewHost(
		libenet.NewListenAddress(h.cfg.ListenPort),
		h.cfg.PeerLimit,
		h.cfg.ChannelLimit,
		0,
		0,
	)
	if err != nil {
		return err
	}
	defer host.Destroy()

	h.logger.Info("enet host listening", "port", h.cfg.ListenPort)

	var mu sync.Mutex
	sessions := make(map[uint16]*peerSession)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		ev := host.Service(h.cfg.ServiceTimeoutMs)
		switch ev.GetType() {
		case libenet.EventNone:
			continue

		case libenet.EventConnect:
			sess := newPeerSession(ev.GetPeer(), h.connectionFactory, h.cfg.EnetIncomingQueueSize)
			mu.Lock()
			sessions[sess.id] = sess
			mu.Unlock()
			h.logger.Info("enet peer connected", "enet_peer_id", sess.id)

		case libenet.EventReceive:
			id := ev.GetPeer().GetIncomingPeerId()
			packet := ev.GetPacket()

			mu.Lock()
			sess, ok := sessions[id]
			mu.Unlock()
			if !ok {
				packet.Destroy()
				h.logger.Warn("enet receive from unknown peer", "enet_peer_id", id)
				continue
			}

			if !sess.admitted {
				data := append([]byte(nil), packet.GetData()...)
				packet.Destroy()
				if h.packetTooLarge(len(data)) {
					h.logger.Warn("enet admit packet too large", "enet_peer_id", id, "bytes", len(data))
					h.disconnectSession(ctx, &mu, sessions, id, sess)
					continue
				}
				if err := h.admit(ctx, sess, data); err != nil {
					deliveryerrors.SendJoinReject(sess.conn, err)
					_ = sess.close()
					sess.clearPeerData()
					mu.Lock()
					delete(sessions, id)
					mu.Unlock()
				}
				continue
			}

			frame, err := frameFromPacket(packet, h.cfg.MaxIncomingFrameBytes)
			packet.Destroy()
			if err != nil {
				if errors.Is(err, domain.ErrIncomingFrameTooLarge) {
					h.logger.Warn("enet frame too large", "enet_peer_id", id)
				} else {
					h.logger.Warn("enet invalid frame", "error", err, "enet_peer_id", id)
				}
				h.disconnectSession(ctx, &mu, sessions, id, sess)
				continue
			}
			if !sess.deliver(frame) {
				h.logger.Warn("enet incoming queue full, dropping frame", "enet_peer_id", id)
			}

		case libenet.EventDisconnect:
			id := ev.GetPeer().GetIncomingPeerId()
			mu.Lock()
			sess, ok := sessions[id]
			delete(sessions, id)
			mu.Unlock()
			if ok {
				if sess.admitted && sess.roomID.IsValid() && sess.peerID.IsValid() {
					if err := h.leaveRoomUseCase.LeaveRoom(ctx, sess.roomID, sess.peerID); err != nil {
						h.logger.Error("enet leave room failed", "error", err, "enet_peer_id", id, "roomID", sess.roomID, "peerID", sess.peerID)
					}
				}
				_ = sess.close()
				sess.clearPeerData()
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

func (h *RoomsHandler) disconnectSession(
	ctx context.Context,
	mu *sync.Mutex,
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
	sess.clearPeerData()
	mu.Lock()
	delete(sessions, id)
	mu.Unlock()
}
