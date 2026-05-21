package realtime

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"

type RelayHandler struct {
	room Room
}

func (h *RelayHandler) OnStart(room Room) error {
	return nil
}

func (h *RelayHandler) OnStop(room Room) error {
	return nil
}

func (h *RelayHandler) OnJoin(peer Peer) error {
	return nil
}

func (h *RelayHandler) OnLeave(peer Peer) error {

	return nil
}

func (h *RelayHandler) OnMessage(roomEvent events.RoomEvent) error {
	room := h.room
	room.Send(events.PeerEvent{
		Frame: roomEvent.Frame,
	})
	return nil
}
