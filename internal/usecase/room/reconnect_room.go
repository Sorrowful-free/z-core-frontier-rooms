package room

import (
	"context"
	"fmt"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

// ReconnectRoomUseCase — the resume flow behind AdmitPeerUseCase (P20):
// reviving a peer under the same peerID within the grace of an admitted
// slot. Differences from the join flow: reservation.Admit is not called
// (the slot is already admitted — the atomic check and lastSeen refresh
// live in reservation.Reconnect), the slot is never revoked on any failure
// (the peer is in grace), and the nick is taken from the slot — the
// reconnect token doesn't carry one. The grace deadline (P24) is checked
// before the refresh via read-only SlotDetails — see ReconnectRoom. The
// double-resume race on one token is settled by Replace (newest-wins, Q10)
// — see placePeer.
type ReconnectRoomUseCase struct {
	peerFactory  realtime.PeerFactory
	roomRegistry registry.RoomRegistry
	reservation  reservation.Reservation
	reconnectTTL time.Duration
	logging      logging.Logger
	metrics      portmetrics.Metrics
}

func NewReconnectRoomUseCase(
	peerFactory realtime.PeerFactory,
	roomRegistry registry.RoomRegistry,
	reservation reservation.Reservation,
	reconnectTTL time.Duration,
	logging logging.Logger,
	m portmetrics.Metrics,
) *ReconnectRoomUseCase {
	return &ReconnectRoomUseCase{
		peerFactory:  peerFactory,
		roomRegistry: roomRegistry,
		reservation:  reservation,
		reconnectTTL: reconnectTTL,
		logging:      logging,
		metrics:      portmetrics.OrNoop(m),
	}
}

func (uc *ReconnectRoomUseCase) ReconnectRoom(ctx context.Context, connection transport.Connection, claims domain.Claims) (PeerSession, error) {
	if err := ctx.Err(); err != nil {
		return PeerSession{}, err
	}

	room, err := uc.roomRegistry.GetRoom(ctx, claims.RoomID)
	if err != nil {
		return PeerSession{}, err
	}

	// Read-only slot lookup (P24): the honest lastSeen BEFORE the refresh —
	// the deadline is computed from it, so a rejected attempt does not
	// extend the grace.
	slot, err := uc.reservation.SlotDetails(ctx, claims.RoomID, claims.PeerID)
	if err != nil {
		uc.logging.Error("reconnect room: reservation slot details failed", "error", err, "roomID", claims.RoomID, "peerID", claims.PeerID)
		return PeerSession{}, err
	}

	// Deadline gate (P24): an admitted slot with no live peer and an
	// expired lastSeen + reconnectTTL → ErrReservationExpired — grace is
	// finite even without a sweep pass; the predicate is shared with the
	// sweep — see graceExpired. A live peer (HasPeer) is untouched by the
	// gate: a long-connected peer's old lastSeen is legitimately stale, and
	// Replace (newest-wins, Q10) proceeds without the gate.
	// reconnectTTL <= 0 → any !HasPeer resume is expired — TTL=0 forbids
	// resurrection specifically, not takeover of a live connection.
	if !room.HasPeer(claims.PeerID) && graceExpired(slot.LastSeen, uc.reconnectTTL, time.Now()) {
		expiredErr := fmt.Errorf("%w: room %d peer %d", domain.ErrReservationExpired, claims.RoomID, claims.PeerID)
		uc.logging.Error("reconnect room: grace deadline expired", "error", expiredErr, "roomID", claims.RoomID, "peerID", claims.PeerID)
		return PeerSession{}, expiredErr
	}

	// Atomic under the room's reservation mutex (T5): admitted slot +
	// lastSeen refresh, otherwise reject — the sweep-revoke ↔ resume race
	// is closed.
	slot, err = uc.reservation.Reconnect(ctx, claims.RoomID, claims.PeerID)
	if err != nil {
		uc.logging.Error("reconnect room: reservation reconnect failed", "error", err, "roomID", claims.RoomID, "peerID", claims.PeerID)
		return PeerSession{}, err
	}

	session, err := placePeerResume(uc.peerFactory, room, connection, claims.PeerID, slot.NickName, uc.logging)
	if err != nil {
		return PeerSession{}, err
	}

	// OpPeerReconnected to the others (Q5): the opcode goes out immediately
	// via room.Send (P34); the add patch reaches recipients on the next
	// patch tick. A broadcast error does not sink the resume — the peer is
	// already in the room and a reject would kill a live connection.
	if err := room.Send(events.PeerEvent{
		ExcludePeerID: claims.PeerID,
		Frame:         domain.NewPeerNotificationFrame(domain.OpPeerReconnected, claims.PeerID),
	}); err != nil {
		uc.logging.Error("reconnect room: broadcast reconnected failed", "error", err, "peerID", claims.PeerID)
	}

	uc.metrics.ReconnectSucceeded()
	uc.logging.Info("reconnect room: reconnect success", "roomID", claims.RoomID, "peerID", claims.PeerID)
	return session, nil
}
