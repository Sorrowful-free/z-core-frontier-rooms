package room

import (
	"context"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
)

// Значение label `sweep` для SweepRan/IdlePeersEvicted (bounded в адаптере).
const sweepKindIdlePeers = "idle_peers"

// EvictIdlePeersUseCase выводит zombie peer'ов с мёртвым транспортом (ADR 0001).
// Вывод идёт через disconnect-поток (P20): zombie получает то же grace-окно —
// полуоткрытый TCP (целевой сценарий флаки-клиентов) не отличается от чистого
// разрыва.
type EvictIdlePeersUseCase struct {
	roomRegistry          registry.RoomRegistry
	disconnectPeerUseCase *DisconnectPeerUseCase
	logger                logging.Logger
	metrics               portmetrics.Metrics
}

func NewEvictIdlePeersUseCase(
	roomRegistry registry.RoomRegistry,
	disconnectPeerUseCase *DisconnectPeerUseCase,
	logger logging.Logger,
	m portmetrics.Metrics,
) *EvictIdlePeersUseCase {
	return &EvictIdlePeersUseCase{
		roomRegistry:          roomRegistry,
		disconnectPeerUseCase: disconnectPeerUseCase,
		logger:                logger,
		metrics:               portmetrics.OrNoop(m),
	}
}

// RunOnce проходит по комнатам и выводит peer'ов с !IsAlive() через disconnect-поток.
func (uc *EvictIdlePeersUseCase) RunOnce(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// Проход sweep считается отдельно от обработанных записей (P19b).
	uc.metrics.SweepRan(sweepKindIdlePeers)

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
			if err := uc.disconnectPeerUseCase.DisconnectPeer(ctx, PeerSession{
				RoomID: roomID,
				PeerID: peer.GetID(),
				Peer:   peer,
			}); err != nil {
				uc.logger.Error("evict idle peer failed", "error", err, "roomID", roomID, "peerID", peer.GetID())
				continue
			}
			evicted++
		}
	}

	if evicted > 0 {
		uc.metrics.IdlePeersEvicted(evicted)
	}
	return evicted, nil
}

// StartIdlePeerSweep запускает фоновый sweep; interval <= 0 — no-op (nil handle).
func StartIdlePeerSweep(
	ctx context.Context,
	uc *EvictIdlePeersUseCase,
	interval time.Duration,
	logger logging.Logger,
) *SweepHandle {
	return startSweep(ctx, interval, logger, "idle peer sweep", "evicted", uc.RunOnce)
}
