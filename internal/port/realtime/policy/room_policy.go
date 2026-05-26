package policy

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type RoomPolicy interface {
	OnStart(room realtime.Room) error
	OnStop(room realtime.Room) error
	OnJoin(peer realtime.Peer) error
	OnLeave(peer realtime.Peer) error
	OnMessage(roomEvent events.RoomEvent) error
}
