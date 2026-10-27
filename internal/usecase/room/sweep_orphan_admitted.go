package room

import (
	"context"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
)

// Значение label `sweep` для SweepRan/OrphanAdmittedRevoked (bounded в адаптере).
const sweepKindOrphanAdmitted = "orphan_admitted"

// SweepOrphanAdmittedUseCase — grace-expiry admitted-слотов без peer в room
// (P20): слот без peer'а и lastSeen (AdmittedSlot.LastSeen) + ReconnectTTL прошёл → Revoke.
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

// RunOnce проходит по комнатам и revoke orphan admitted (нет peer, TTL истёк).
func (uc *SweepOrphanAdmittedUseCase) RunOnce(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// Проход sweep считается отдельно от обработанных записей (P19b).
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

		// P31: revoke последнего grace-слота — именно здесь «пустая»
		// наступает по истечении TTL последнего reconnect-окна: ни живых
		// peers, ни admitted-слотов → комната умирает.
		if roomRevoked {
			closeRoomIfEmpty(ctx, room, uc.roomRegistry, uc.reservation, uc.logger)
		}
	}

	if revoked > 0 {
		uc.metrics.OrphanAdmittedRevoked(revoked)
	}
	return revoked, nil
}

// StartOrphanAdmittedSweep запускает фоновый sweep; interval <= 0 — no-op (nil handle).
func StartOrphanAdmittedSweep(
	ctx context.Context,
	uc *SweepOrphanAdmittedUseCase,
	interval time.Duration,
	logger logging.Logger,
) *SweepHandle {
	return startSweep(ctx, interval, logger, "orphan admitted sweep", "revoked", uc.RunOnce)
}
