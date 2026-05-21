package realtime

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

type RoomHandler interface {
	OnStart(room Room) error
	OnStop(room Room) error

	OnJoin(peer Peer) error
	OnLeave(peer Peer) error

	OnMessage(roomEvent events.RoomEvent) error
}
