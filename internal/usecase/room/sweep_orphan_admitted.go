package room

import (
	"context"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
)

// Value of the `sweep` label for SweepRan/OrphanAdmittedRevoked (bounded in the adapter).
const sweepKindOrphanAdmitted = "orphan_admitted"

// SweepOrphanAdmittedUseCase — grace expiry of admitted slots with no peer
// in the room (P20): a slot with no peer and a lastSeen
// (AdmittedSlot.LastSeen) whose ReconnectTTL has elapsed → Revoke.
type SweepOrphanAdmittedUseCase struct {
	roomRegistry registry.RoomRegistry
	reservation  reservation.Reservation
	reconnectTTL time.Duration
	logger       logging.Logger
	metrics      portmetrics.Metrics
}

func NewSweepOrphanAdmittedUseCase(
	roomRegistry registry.RoomRegistry,
	reservation reservation.Reservation,
	reconnectTTL time.Duration,
	logger logging.Logger,
	m portmetrics.Metrics,
) *SweepOrphanAdmittedUseCase {
	return &SweepOrphanAdmittedUseCase{
		roomRegistry: roomRegistry,
		reservation:  reservation,
		reconnectTTL: reconnectTTL,
		logger:       logger,
		metrics:      portmetrics.OrNoop(m),
	}
}

// RunOnce iterates over rooms and revokes orphan admitted slots (no peer, TTL expired).
func (uc *SweepOrphanAdmittedUseCase) RunOnce(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// A sweep pass is counted separately from processed records (P19b).
	uc.metrics.SweepRan(sweepKindOrphanAdmitted)

	rooms, err := uc.roomRegistry.GetList(ctx)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	revoked := 0

	for _, room := range rooms {
		if err := ctx.Err(); err != nil {
			return revoked, err
		}
		roomID := room.GetID()
		slots, err := uc.reservation.ListAdmittedPeers(ctx, roomID)
		if err != nil {
			uc.logger.Error("list admitted peers failed", "error", err, "roomID", roomID)
			continue
		}

		roomRevoked := false
		for _, slot := range slots {
			if room.HasPeer(slot.PeerID) {
				continue
			}
			if !graceExpired(slot.LastSeen, uc.reconnectTTL, now) {
				continue
			}

			uc.logger.Warn("sweep orphan admitted slot", "roomID", roomID, "peerID", slot.PeerID)
			if err := uc.reservation.Revoke(ctx, roomID, slot.PeerID); err != nil {
				uc.logger.Error("revoke orphan admitted slot failed", "error", err, "roomID", roomID, "peerID", slot.PeerID)
				continue
			}
			revoked++
			roomRevoked = true
		}

		// P31: revoking the last grace slot — this is where "empty" is
		// reached when the TTL of the last reconnect window expires: no live
		// peers, no admitted slots → the room dies.
		if roomRevoked {
			closeRoomIfEmpty(ctx, room, uc.roomRegistry, uc.reservation, uc.logger)
		}
	}

	if revoked > 0 {
		uc.metrics.OrphanAdmittedRevoked(revoked)
	}
	return revoked, nil
}

// StartOrphanAdmittedSweep starts the background sweep; interval <= 0 — no-op (nil handle).
func StartOrphanAdmittedSweep(
	ctx context.Context,
	uc *SweepOrphanAdmittedUseCase,
	interval time.Duration,
	logger logging.Logger,
) *SweepHandle {
	return startSweep(ctx, interval, logger, "orphan admitted sweep", "revoked", uc.RunOnce)
}
