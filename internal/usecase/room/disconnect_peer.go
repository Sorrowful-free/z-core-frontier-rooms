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

// DisconnectPeerUseCase — connection drop without losing identity (P20):
// the peer is removed from the room via the regular leave path (its state
// record is cleaned, election is instant, a remove patch goes to the
// others), but the reservation slot is not revoked — MarkDisconnected sets
// lastSeen, from which the sweep counts down the RESERVATION_RECONNECT_TTL
// grace window; the others get OpPeerDisconnected. All disconnect triggers
// (WS waiter, ENet EventDisconnect/bad-frame, idle sweep) land here;
// LeaveRoomUseCase remains for explicit leaves and rollbacks.
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

	// Identity pre-check before MarkDisconnected: session.Peer may be an
	// old object displaced by Replace/resume (stale teardown) — the
	// lastSeen mark and peer_disconnects_total belong to the live peer, not
	// to it. The narrow window before Leave is closed by ErrPeerStale in
	// the lifecycle.
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

	// Mark before leave: while the peer is in the room the sweep ignores
	// the slot (HasPeer) — the lastSeen mark is set with no "orphan slot
	// without a timestamp" window. A mark error does not block removing
	// the peer from the room.
	// Known issue: two concurrent teardowns of the same session (idle
	// sweep ∥ WS waiter / ENet EventDisconnect) can both pass the identity
	// pre-check above and both reach the mark — a double mark
	// (peer_disconnects_total +2, lastSeen refreshed twice). The broadcast
	// stays single though: the losing room.Leave gets ErrPeerNotFound and
	// exits before the send. Accepted as benign noise, no dedup added.
	// Stale-mark — a race of the same class: the mark sits between the
	// identity pre-check and the authoritative Leave, so if the peer is
	// displaced by Replace in this window the mark lands on the new peer's
	// slot (metric +1, lastSeen refreshed), and Leave then answers
	// ErrPeerStale.
	if err := uc.reservation.MarkDisconnected(ctx, session.RoomID, session.PeerID); err != nil {
		uc.logging.Error("disconnect peer: mark disconnected failed", "error", err, "roomID", session.RoomID, "peerID", session.PeerID)
	}

	leaveErr := room.Leave(session.Peer)
	switch {
	case errors.Is(leaveErr, domain.ErrPeerStale):
		// Expected race (T8): the old conn woke up after Replace — the
		// identity belongs to the new peer; nothing is removed or broadcast.
		uc.logging.Debug("disconnect peer: stale teardown", "roomID", session.RoomID, "peerID", session.PeerID)
		return nil
	case errors.Is(leaveErr, domain.ErrPeerNotFound):
		uc.logging.Warn("disconnect peer: peer already left", "roomID", session.RoomID, "peerID", session.PeerID)
		return nil
	case errors.Is(leaveErr, domain.ErrRoomShouldClose):
		// Authoritative host (P29): a master disconnect kills the room at
		// once — no grace window for the host; the rest get OpRoomClosed and
		// a disconnect, and the teardown revokes the slots (including the one
		// just marked).
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

	// The opcode goes out immediately via room.Send (P34); the remove
	// patch reaches recipients on the next patch tick.
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
	// P31: disconnecting the last peer usually leaves a grace slot
	// admitted → closeRoomIfEmpty is a no-op and the room lives until
	// RESERVATION_RECONNECT_TTL expires; the kill here only fires when the
	// grace slot is already gone (e.g. IssueTicket revoked it).
	closeRoomIfEmpty(ctx, room, uc.roomRegistry, uc.reservation, uc.logging)
	return nil
}
