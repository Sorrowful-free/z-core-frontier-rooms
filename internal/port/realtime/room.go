package realtime

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

type Room interface {
	Context() context.Context

	GetID() domain.RoomID
	GetCapacity() int8
	GetPeers() []Peer

	Start() error
	Stop() error

	Join(peer Peer) error
	Leave(peer Peer) error
	Replace(peer Peer) error
	HasPeer(peerID domain.PeerID) bool
	GetPeer(peerID domain.PeerID) (Peer, error)

	Send(peerEvent events.PeerEvent) error
	Deliver(roomEvent events.RoomEvent) error
}
