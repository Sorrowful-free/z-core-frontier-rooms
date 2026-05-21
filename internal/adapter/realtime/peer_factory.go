package realtime

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

type PeerFactory struct {
	logger logging.Logger
}

func NewPeerFactory(logger logging.Logger) *PeerFactory {
	return &PeerFactory{
		logger: logger,
	}
}
func (f *PeerFactory) CreatePeer(id domain.PeerID, connection transport.Connection) realtime.Peer {
	return NewPeer(id, connection, f.logger)
}
