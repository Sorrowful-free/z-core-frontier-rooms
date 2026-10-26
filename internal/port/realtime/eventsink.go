package realtime

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

// EventSink — узкая точка доставки входящих кадров в комнату
// (mailbox room.incoming); реализует Room. Peer получает sink
// в Start и не хранит ссылки на комнату (P26).
type EventSink interface {
	Deliver(events.RoomEvent) error
}
