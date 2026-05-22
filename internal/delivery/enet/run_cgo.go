//go:build enet && cgo

package enet

import (
	"context"
	"errors"
	"sync"

	enetconn "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	libenet "github.com/codecat/go-enet"
)

var errEmptyToken = errors.New("enet: empty token")

type peerSession struct {
	id       uint16
	peer     libenet.Peer
	incoming chan domain.Frame
	conn     *enetconn.EnetConnection
	admitted bool
}

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
			peer := ev.GetPeer()
			id := peer.GetIncomingPeerId()
			incoming := make(chan domain.Frame, 256)
			conn := enetconn.NewEnetConnection(peer, incoming)
			sess := &peerSession{
				id:       id,
				peer:     peer,
				incoming: incoming,
				conn:     conn,
			}
			mu.Lock()
			sessions[id] = sess
			mu.Unlock()
			h.logger.Info("enet peer connected", "peer_id", id)

		case libenet.EventReceive:
			peer := ev.GetPeer()
			id := peer.GetIncomingPeerId()
			packet := ev.GetPacket()
			flags := packet.GetFlags()
			data := append([]byte(nil), packet.GetData()...)
			packet.Destroy()

			mu.Lock()
			sess, ok := sessions[id]
			mu.Unlock()
			if !ok {
				h.logger.Warn("enet receive from unknown peer", "peer_id", id)
				continue
			}

			if !sess.admitted {
				if err := h.admit(ctx, sess, data); err != nil {
					_ = sess.conn.Close()
					peer.SetData(nil)
					mu.Lock()
					delete(sessions, id)
					mu.Unlock()
				}
				continue
			}

			frame, err := frameFromPacket(data)
			if err != nil {
				h.logger.Warn("enet invalid frame", "error", err, "peer_id", id)
				continue
			}
			select {
			case sess.incoming <- frame:
			default:
				h.logger.Warn("enet incoming queue full, dropping frame", "peer_id", id)
			}

		case libenet.EventDisconnect:
			peer := ev.GetPeer()
			id := peer.GetIncomingPeerId()
			mu.Lock()
			sess, ok := sessions[id]
			delete(sessions, id)
			mu.Unlock()
			if ok {
				_ = sess.conn.Close()
				peer.SetData(nil)
				h.logger.Info("enet peer disconnected", "peer_id", id, "data", ev.GetData())
			}
		}
	}
}

func (h *RoomsHandler) admit(ctx context.Context, sess *peerSession, token []byte) error {
	if len(token) == 0 {
		h.logger.Warn("enet connect: empty token", "peer_id", sess.id)
		return errEmptyToken
	}

	if _, err := h.connectUseCase.Connect(ctx, sess.conn, token); err != nil {
		h.logger.Error("enet connect failed", "error", err, "peer_id", sess.id)
		return err
	}

	sess.admitted = true
	h.logger.Info("enet peer admitted", "peer_id", sess.id)
	return nil
}
