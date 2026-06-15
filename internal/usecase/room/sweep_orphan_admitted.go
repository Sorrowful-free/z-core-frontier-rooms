package room

import (
	"context"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/reservation"
)

// SweepOrphanAdmittedUseCase освобождает admitted-слоты без peer в room (шаг 4A).
type SweepOrphanAdmittedUseCase struct {
	roomRegistry registry.RoomRegistry
	reservation  reservation.Reservation
	orphanTTL    time.Duration
	logger       logging.Logger
}

func NewSweepOrphanAdmittedUseCase(
	roomRegistry registry.RoomRegistry,
	reservation reservation.Reservation,
	orphanTTL time.Duration,
	logger logging.Logger,
) *SweepOrphanAdmittedUseCase {
	return &SweepOrphanAdmittedUseCase{
		roomRegistry: roomRegistry,
		reservation:  reservation,
		orphanTTL:    orphanTTL,
		logger:       logger,
	}
}

// RunOnce проходит по комнатам и revoke orphan admitted (нет peer, TTL истёк).
func (uc *SweepOrphanAdmittedUseCase) RunOnce(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	rooms, err := uc.roomRegistry.GetList(ctx)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	revoked := 0

	for _, room := range rooms {
		roomID := room.GetID()
		slots, err := uc.reservation.ListAdmittedPeers(ctx, roomID)
		if err != nil {
			return revoked, err
		}

		for _, slot := range slots {
			if room.HasPeer(slot.PeerID) {
				continue
			}
			if uc.orphanTTL > 0 && now.Before(slot.AdmittedAt.Add(uc.orphanTTL)) {
				continue
			}

			uc.logger.Warn("sweep orphan admitted slot", "roomID", roomID, "peerID", slot.PeerID)
			if err := uc.reservation.Revoke(ctx, roomID, slot.PeerID); err != nil {
				return revoked, err
			}
			revoked++
		}
	}

	return revoked, nil
}

// StartOrphanAdmittedSweep запускает фоновый sweep; interval <= 0 — no-op.
func StartOrphanAdmittedSweep(
	ctx context.Context,
	uc *SweepOrphanAdmittedUseCase,
	interval time.Duration,
	logger logging.Logger,
) {
	if interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				revoked, err := uc.RunOnce(ctx)
				if err != nil && ctx.Err() == nil {
					logger.Error("orphan admitted sweep failed", "error", err)
					continue
				}
				if revoked > 0 {
					logger.Info("orphan admitted sweep complete", "revoked", revoked)
				}
			}
		}
	}()
}
