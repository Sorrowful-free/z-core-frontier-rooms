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

// All libenet calls (Host/Peer/Packet) are serialized by the owning host's
// HostLoop (adapter/transport/enet/hostloop_cgo.go): host.Service goes as a
// blocking Call command into the loop; peer/packet operations inside dispatch
// - Call/Do into the same loop. Dispatch itself runs on the run goroutine,
// NOT on the loop goroutine: the admit/teardown path (AdmitPeer/
// DisconnectPeer → dispatchLifecycle) synchronously waits on the room
// goroutine, which in OnJoin/OnLeave performs EnetConnection.Ping - a
// blocking Call into the same loop. Dispatch on the loop goroutine would
// close a cross-deadlock executor↔room; on the run goroutine the blocking is
// harmless, and the executor stays free to serve Ping/conn operations.
// Only the run goroutine touches sessions and peerSession fields - no mutex
// needed.
//
// P18: run executes one goroutine per host (port); all hosts share the mux's
// common ctx (Listen). A single host's ceiling is PeerLimit and its
// HostLoop's pps; room capacity is not tied to a host (the room is
// transport-agnostic).
func (h *RoomsHandler) run(ctx context.Context, host *enetHost) error {
	// Initialize is idempotent (refcount inside libenet): the mux's N run
	// goroutines call it without a lock; Deinitialize only drops its own
	// refcount, the real teardown happens after all loops exit.
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
	// Stop is idempotent and waits for the executor to exit (drain +
	// host.Destroy); before Deinitialize - reverse order.
	defer loop.Stop()
	// Peer→host binding: each host gets its OWN factory (WithLoop clones the
	// injected one - a shared factory with BindLoop across N hosts would route
	// conns into another host's loop).
	connFactory := h.connectionFactory
	if f, ok := h.connectionFactory.(*transportenet.EnetConnectionFactory); ok {
		connFactory = f.WithLoop(loop)
	}

	h.logger.Info("enet host listening", "port", host.port, "hostID", host.id)

	sessions := make(map[uint16]*peerSession)

	for {
		if err := ctx.Err(); err != nil {
			h.closeSessions(sessions)
			// Best-effort drain: conn.Close from closeSessions queued a
			// graceful DisconnectLater on the loop (FIFO - it will execute
			// before the service commands below); a couple of service
			// iterations push them out on the wire before Destroy.
			h.drainHost(loop)
			return err
		}

		h.disconnectExpiredAdmits(sessions)

		ev, err := transportenet.Call(loop, func() libenet.Event {
			return loop.Host().Service(h.cfg.ServiceTimeoutMs)
		})
		if err != nil {
			// Loop stopped from the outside: the shutdown sequence above has
			// already run or is not needed (the host is on its way to Destroy).
			h.closeSessions(sessions)
			return transportenet.ErrHostLoopStopped
		}
		h.dispatchEvent(ctx, host, loop, connFactory, sessions, ev)
	}
}

// dispatchEvent - handles one Service event on the run goroutine; peer/packet
// access - via Call/Do into the loop (the Event accessors GetType/GetPeer/
// GetPacket/GetData read the Go copy of the event and need no dispatch).
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
				// The counter counts sent reject frames, not attempts
				// (P19b): emitted only on a successful send; an error goes to
				// the log.
				if sendErr := deliveryerrors.SendJoinReject(sess.conn, err); sendErr != nil {
					h.logger.Warn("enet join reject send failed", "error", sendErr, "enet_peer_id", id)
				} else {
					h.metrics.JoinRejected(deliveryerrors.JoinRejectOpCode(err))
				}
				// Graceful close: conn.Close queues DisconnectLater on the
				// loop - first the queued reliable reject goes out on the wire
				// (same FIFO), then the disconnect (P7: the reject arrives).
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
			if sess.admitted && sess.roomSession.PeerID.IsValid() {
				if err := h.disconnectPeerUseCase.DisconnectPeer(ctx, sess.roomSession); err != nil {
					h.logger.Error("enet disconnect peer failed", "error", err, "enet_peer_id", id, "roomID", sess.roomSession.RoomID, "peerID", sess.roomSession.PeerID)
				}
			}
			_ = sess.close()
			h.logger.Info("enet peer disconnected", "enet_peer_id", id, "data", ev.GetData())
		}
	}
}

// admit - the peer's first packet: the single admit entry point (P20).
// Strict binding (P18) is checked by the facade: the ticket's claims.HostID
// must match the host that accepted the connection, otherwise join-reject
// (ErrWrongHost → OpWrongHost 0x77) before spending a reservation slot.
// A ticket without hostID passes on any host.
func (h *RoomsHandler) admit(ctx context.Context, host *enetHost, sess *peerSession, token []byte) error {
	if len(token) == 0 {
		h.logger.Warn("enet connect: empty token", "enet_peer_id", sess.id)
		return domain.ErrEmptyToken
	}

	session, err := h.admitPeerUseCase.Admit(ctx, sess.conn, token, host.id)
	if err != nil {
		h.logger.Error("enet admit failed", "error", err, "enet_peer_id", sess.id, "op", deliveryerrors.JoinRejectOpCode(err))
		return err
	}

	sess.admitted = true
	sess.roomSession = session
	h.logger.Info("enet peer admitted", "enet_peer_id", sess.id, "hostID", host.id)
	return nil
}

func (h *RoomsHandler) packetTooLarge(size int) bool {
	max := h.cfg.MaxIncomingFrameBytes
	return max > 0 && size > max
}

// disconnectSession - DisconnectPeer (if admitted, P20) + graceful close +
// removal from the map. conn.Close queues a graceful peer.Disconnect on the
// loop: queued outbound packets go out before the break (FIFO preserves the
// reject → disconnect order).
func (h *RoomsHandler) disconnectSession(
	ctx context.Context,
	sessions map[uint16]*peerSession,
	id uint16,
	sess *peerSession,
) {
	if sess.admitted && sess.roomSession.PeerID.IsValid() {
		if err := h.disconnectPeerUseCase.DisconnectPeer(ctx, sess.roomSession); err != nil {
			h.logger.Error("enet disconnect peer failed", "error", err, "enet_peer_id", id, "roomID", sess.roomSession.RoomID, "peerID", sess.roomSession.PeerID)
		}
	}
	_ = sess.close()
	delete(sessions, id)
}

// disconnectExpiredAdmits disconnects peers that took a PeerLimit slot on
// EventConnect but did not pass admit within cfg.AdmitTimeout. Admitted
// sessions are left alone - the idle peer sweep reaps them.
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

// closeSessions closes all sessions at shutdown: conn.Close closes the
// closed channel (waking the peers' Receive goroutines) and queues a graceful
// disconnect on the loop - drainHost afterwards pushes them out on the wire.
// LeaveRoom/Revoke for admitted peers is not performed here - the registry
// shutdown (DeleteForShutdown) removes them before Stop is called; the
// EventDisconnect that drainHost swallows does not run the leave path either.
func (h *RoomsHandler) closeSessions(sessions map[uint16]*peerSession) {
	for id, sess := range sessions {
		_ = sess.close()
		delete(sessions, id)
	}
}

// drainHost - best-effort: only 2 service iterations (~2xServiceTimeoutMs)
// through the loop's queue before Stop/Destroy. A peer with a large backlog
// may not manage to flush its queue and will get a hard disconnect instead of
// a graceful one - acceptable: the shutdown path already signals process
// termination, and waiting arbitrarily long is not an option. Call errors
// (loop already stopped) are ignored - the path is best-effort.
func (h *RoomsHandler) drainHost(loop *transportenet.HostLoop) {
	for range 2 {
		_, _ = transportenet.Call(loop, func() libenet.Event {
			return loop.Host().Service(h.cfg.ServiceTimeoutMs)
		})
	}
}
