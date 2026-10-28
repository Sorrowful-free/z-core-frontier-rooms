package realtime

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

type PeerFactory interface {
	// CreatePeer constructs a peer on top of connection; ctx is the peer's
	// lifecycle (caller passes room.Context()), no room is needed for
	// construction — the mailbox entry point arrives in Peer.Start (P26).
	CreatePeer(ctx context.Context, id domain.PeerID, nickName string, connection transport.Connection, logger logging.Logger) (Peer, error)
}
