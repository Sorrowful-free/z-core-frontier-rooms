package realtime

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

// EventSink — narrow delivery point for inbound frames into a room
// (the room.incoming mailbox); implemented by Room. Peer receives the
// sink in Start and does not hold a reference to the room (P26).
type EventSink interface {
	Deliver(events.RoomEvent) error
}
