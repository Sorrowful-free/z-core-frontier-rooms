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

// closeRoom — room teardown on policy request (ErrRoomShouldClose, P33,
// allowMasterMigration=false: master left — the room dies). Same steps as
// DeleteRoom, plus a terminal wire frame: all remaining peers get
// OpRoomClosed (0x82, empty payload), then unregister from registry, revoke
// of reservation slots (UnregisterRoom also covers the departed master's
// slot) and disconnect of each peer.
//
// Order matters:
//   - Deliver before DeleteRoom: a peer is created on room.Context() — room
//     Stop cancels the peer ctx too, after which tryDeliver returns "peer
//     stopped".
//   - DeleteRoom before peer.Stop: conn close triggers the transport
//     callback (ws waiter / ENet EventDisconnect → DisconnectPeer); when the
//     room is already off the registry, the callback hits ErrRoomNotFound and
//     does not pile a grace mark / OpPeerDisconnected on top of a dying room.
//   - Stop last: drain-aware peer.Stop (P30) guarantees the order
//     "Send of the committed frame before conn.Close" — the write loop drains
//     outbound within the drain timeout before closing the connection (on ENet
//     SendBytes and DisconnectLater share the host loop's single FIFO queue,
//     so the order is strict de jure; on WS Send is called before Close and
//     buffer flushing is up to the kernel).
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
	// A frame-flush window is no longer needed: drain-aware peer.Stop (P30)
	// calls conn.Close only after the write loop has drained the outbound
	// committed before Stop (bounded by the drain timeout) — the
	// "Send 0x82 before Close" order is guaranteed at the peer level, not by
	// a scheduling trick.

	if err := roomRegistry.DeleteRoom(ctx, roomID); err != nil {
		if errors.Is(err, domain.ErrRoomNotFound) {
			// Race with a parallel teardown (DeleteRoom / a second death path):
			// the room is already off the registry — goal achieved, not an error.
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

	// Stops run in parallel: drain-aware Stop (P30) waits for writeDone up to
	// the drain timeout on a blocked Send; a serial loop would give
	// O(N×timeout) teardown (capacity up to 127).
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

// closeRoomIfEmpty — empty-room death (P31): teardown after the last peer is
// actually removed. "Empty" = neither live peers in the room nor admitted
// reservation slots: a grace slot (MarkDisconnected) stays admitted and
// keeps reconnect possible — the room lives until
// RESERVATION_RECONNECT_TTL expires, when the orphan sweep revokes the slot.
//
// Pending reserve tickets do not block the kill (unlike the SweepEmptyRooms
// predicate): the room dies once everyone who joined has left — a late
// joiner gets a reject (0x43 / a reservation error), not an empty room.
//
// The kill is idempotent via DeleteRoom (ErrRoomNotFound is tolerated, as in
// closeRoom). The "join landed in a dying room" race is the same as for
// DELETE: the peer lands in a dead room and its ctx dies with room.Stop —
// accepted without extra guarantees. Errors are logged and not propagated:
// an empty-room death must not fail an already completed leave/disconnect.
// Returns true if the teardown was actually performed.
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
		// ErrReservationNotFound — the room was already unregistered from
		// reservation by a parallel teardown: no slots, kill is legitimate.
		// Any other error — state unknown, the kill is deferred until the
		// next trigger/sweep.
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
			// Race with a parallel death (closeRoom, DELETE, sweep):
			// the room is already off the registry — goal achieved.
			logger.Warn("empty room: room already unregistered", "roomID", roomID)
		} else {
			logger.Error("empty room: delete room failed", "error", err, "roomID", roomID)
			killed = false
		}
	}
	if err := reservation.UnregisterRoom(ctx, roomID); err != nil {
		if errors.Is(err, domain.ErrReservationNotFound) {
			// Same idempotency as DeleteRoom: the slots were already
			// released by a parallel teardown — goal achieved.
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
