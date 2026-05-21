package realtime

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

type Peer interface {
	GetID() domain.PeerID

	Start() error
	Stop() error

	GetIncoming() chan<- events.PeerEvent
}
