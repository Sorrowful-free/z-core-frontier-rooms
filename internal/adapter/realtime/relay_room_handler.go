package realtime

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type RelayRoomHandler struct {
	room   realtime.Room
	logger logging.Logger
}

func NewRelayRoomHandler(logger logging.Logger) *RelayRoomHandler {
	return &RelayRoomHandler{
		logger: logger,
	}
}

func (h *RelayRoomHandler) OnStart(room realtime.Room) error {
	h.room = room
	return nil
}

func (h *RelayRoomHandler) OnStop(room realtime.Room) error {
	h.room = nil
	return nil
}

func (h *RelayRoomHandler) OnJoin(peer realtime.Peer) error {
	return nil
}

func (h *RelayRoomHandler) OnLeave(peer realtime.Peer) error {

	return nil
}

func (h *RelayRoomHandler) OnMessage(roomEvent events.RoomEvent) error {
	room := h.room
	room.Send(events.PeerEvent{
		Frame: roomEvent.Frame,
	})
	return nil
}
