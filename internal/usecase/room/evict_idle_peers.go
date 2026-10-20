package room

import (
	"context"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
)

// EvictIdlePeersUseCase выводит zombie peer'ов с мёртвым транспортом (ADR 0001).
type EvictIdlePeersUseCase struct {
	roomRegistry     registry.RoomRegistry
	leaveRoomUseCase *LeaveRoomUseCase
	logger           logging.Logger
}

func NewEvictIdlePeersUseCase(
	roomRegistry registry.RoomRegistry,
	leaveRoomUseCase *LeaveRoomUseCase,
	logger logging.Logger,
) *EvictIdlePeersUseCase {
	return &EvictIdlePeersUseCase{
		roomRegistry:     roomRegistry,
		leaveRoomUseCase: leaveRoomUseCase,
		logger:           logger,
	}
}

// RunOnce проходит по комнатам и выводит peer'ов с !IsAlive() через leave-поток.
func (uc *EvictIdlePeersUseCase) RunOnce(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	rooms, err := uc.roomRegistry.GetList(ctx)
	if err != nil {
		return 0, err
	}

	evicted := 0

	for _, room := range rooms {
		if err := ctx.Err(); err != nil {
			return evicted, err
		}
		roomID := room.GetID()
		for _, peer := range room.GetPeers() {
			if peer.IsAlive() {
				continue
			}

			uc.logger.Warn("evict idle peer", "roomID", roomID, "peerID", peer.GetID())
			if err := uc.leaveRoomUseCase.LeaveRoom(ctx, roomID, peer.GetID()); err != nil {
				uc.logger.Error("evict idle peer failed", "error", err, "roomID", roomID, "peerID", peer.GetID())
				continue
			}
			evicted++
		}
	}

	return evicted, nil
}

// StartIdlePeerSweep запускает фоновый sweep; interval <= 0 — no-op.
func StartIdlePeerSweep(
	ctx context.Context,
	uc *EvictIdlePeersUseCase,
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
				evicted, err := uc.RunOnce(ctx)
				if err != nil && ctx.Err() == nil {
					logger.Error("idle peer sweep failed", "error", err)
					continue
				}
				if evicted > 0 {
					logger.Info("idle peer sweep complete", "evicted", evicted)
				}
			}
		}
	}()
}
