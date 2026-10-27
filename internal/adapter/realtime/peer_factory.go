package realtime

import (
	"context"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

type PeerFactory struct {
	logger        logging.Logger
	outboundQueue int
	drainTimeout  time.Duration
	metrics       portmetrics.Metrics
}

func NewPeerFactory(logger logging.Logger, outboundQueue int, drainTimeout time.Duration, m portmetrics.Metrics) *PeerFactory {
	return &PeerFactory{
		logger:        logger,
		outboundQueue: outboundQueue,
		drainTimeout:  drainTimeout,
		metrics:       portmetrics.OrNoop(m),
	}
}

func (f *PeerFactory) CreatePeer(ctx context.Context, id domain.PeerID, nickName string, connection transport.Connection, logger logging.Logger) (realtime.Peer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if logger == nil {
		logger = f.logger
	}
	peer := NewPeer(ctx, id, nickName, connection, f.outboundQueue, logger)
	peer.metrics = f.metrics
	peer.drainTimeout = f.drainTimeout
	return peer, nil
}
