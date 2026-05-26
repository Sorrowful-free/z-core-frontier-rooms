package realtime

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

type PeerFactory interface {
	CreatePeer(ctx context.Context, id domain.PeerID, connection transport.Connection, room Room, logger logging.Logger) (Peer, error)
}
