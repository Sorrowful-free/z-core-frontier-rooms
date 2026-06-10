package relay

import (
	"fmt"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type RelayRoomPolicy struct {
	room   realtime.Room
	logger logging.Logger
}

func NewRelayRoomPolicy(logger logging.Logger) *RelayRoomPolicy {
	return &RelayRoomPolicy{
		logger: logger,
	}
}

func (h *RelayRoomPolicy) OnStart(room realtime.Room) error {
	h.room = room
	return nil
}

func (h *RelayRoomPolicy) OnStop(room realtime.Room) error {
	h.room = nil
	return nil
}

func (h *RelayRoomPolicy) OnJoin(peer realtime.Peer) error {
	return nil
}

func (h *RelayRoomPolicy) OnLeave(peer realtime.Peer) error {

	return nil
}

func (h *RelayRoomPolicy) OnMessage(roomEvent events.RoomEvent) error {

	room := h.room
	if room == nil {
		return fmt.Errorf("room not started")
	}
	room.Send(events.PeerEvent{
		ExcludePeerID: roomEvent.PeerID,
		Frame:         roomEvent.Frame,
	})
	return nil
}

func (h *RelayRoomPolicy) TickIntervals() (time.Duration, time.Duration) {
	return 0, 0
}

func (h *RelayRoomPolicy) OnTickFullState() error  { return nil }
func (h *RelayRoomPolicy) OnTickPatchState() error { return nil }
