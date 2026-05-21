package realtime

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

type Room interface {
	GetID() domain.RoomID

	Start() error
	Stop() error

	Join(peer Peer) error
	Leave(peer Peer) error

	Send(peerEvent events.PeerEvent) error
	GetIncoming() chan<- events.RoomEvent
}
