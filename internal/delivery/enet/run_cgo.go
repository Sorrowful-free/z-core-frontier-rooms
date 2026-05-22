//go:build enet && cgo

package enet

import (
	"context"
	"errors"
	"sync"

	libenet "github.com/codecat/go-enet"
)

var errEmptyToken = errors.New("enet: empty token")

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
			sess := newPeerSession(ev.GetPeer(), 256)
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
				if err := h.admit(ctx, sess, data); err != nil {
					_ = sess.close()
					sess.clearPeerData()
					mu.Lock()
					delete(sessions, id)
					mu.Unlock()
				}
				continue
			}

			frame, err := frameFromPacket(packet)
			packet.Destroy()
			if err != nil {
				h.logger.Warn("enet invalid frame", "error", err, "enet_peer_id", id)
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
		return errEmptyToken
	}

	summary, peerID, err := h.joinRoomUseCase.JoinRoom(ctx, sess.conn, token)
	if err != nil {
		h.logger.Error("enet join room failed", "error", err, "enet_peer_id", sess.id)
		return err
	}

	sess.admitted = true
	sess.roomID = summary.ID
	sess.peerID = peerID
	h.logger.Info("enet peer admitted", "enet_peer_id", sess.id)
	return nil
}
