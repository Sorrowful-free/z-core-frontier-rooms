package realtime

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

type Peer interface {
	GetID() domain.PeerID
	GetNickName() string
	GetPing() int64

	Start() error
	Stop() error

	Deliver(peerEvent events.PeerEvent) error
}
