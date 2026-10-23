package realtime

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

type PeerFactory struct {
	logger        logging.Logger
	outboundQueue int
	metrics       portmetrics.Metrics
}

func NewPeerFactory(logger logging.Logger, outboundQueue int, m portmetrics.Metrics) *PeerFactory {
	return &PeerFactory{
		logger:        logger,
		outboundQueue: outboundQueue,
		metrics:       portmetrics.OrNoop(m),
	}
}

func (f *PeerFactory) CreatePeer(ctx context.Context, id domain.PeerID, nickName string, connection transport.Connection, room realtime.Room, logger logging.Logger) (realtime.Peer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if logger == nil {
		logger = f.logger
	}
	peer := NewPeer(ctx, id, nickName, connection, room, f.outboundQueue, logger)
	peer.metrics = f.metrics
	return peer, nil
}
