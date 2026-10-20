package realtime

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

type Peer interface {
	GetID() domain.PeerID
	GetNickName() string
	Ping() int64
	// IsAlive — false: транспорт peer'а мёртв, peer подлежит idle eviction.
	IsAlive() bool

	Start() error
	Stop() error

	Deliver(peerEvent events.PeerEvent) error
}
