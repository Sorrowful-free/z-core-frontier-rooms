package room

import (
	"context"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/registry"
)

// Value of the `sweep` label for SweepRan/IdlePeersEvicted (bounded in the adapter).
const sweepKindIdlePeers = "idle_peers"

// EvictIdlePeersUseCase evicts zombie peers with a dead transport (ADR 0001).
// Eviction goes through the disconnect flow (P20): a zombie gets the same
// grace window — a half-open TCP (the target flaky-client scenario) is
// indistinguishable from a clean drop.
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

// RunOnce iterates over rooms and evicts peers with !IsAlive() via the disconnect flow.
func (uc *EvictIdlePeersUseCase) RunOnce(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// A sweep pass is counted separately from processed records (P19b).
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
		// closeRoomIfEmpty is not needed here: DisconnectPeer already does
		// the empty check for each evicted peer; an unconditional call would
		// kill a never-joined room (peers==0, live reserve ticket) on the
		// very first sweep pass — its kill is SweepEmptyRooms' job, driven
		// by slots.
	}

	if evicted > 0 {
		uc.metrics.IdlePeersEvicted(evicted)
	}
	return evicted, nil
}

// StartIdlePeerSweep starts the background sweep; interval <= 0 — no-op (nil handle).
func StartIdlePeerSweep(
	ctx context.Context,
	uc *EvictIdlePeersUseCase,
	interval time.Duration,
	logger logging.Logger,
) *SweepHandle {
	return startSweep(ctx, interval, logger, "idle peer sweep", "evicted", uc.RunOnce)
}
