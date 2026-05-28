package policy

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type StateRoomPolicy struct {
	room   realtime.Room
	state  state.RoomState
	logger logging.Logger
}

func NewStateRoomPolicy(logger logging.Logger) *StateRoomPolicy {
	return &StateRoomPolicy{
		state:  *state.NewRoomState(domain.RoomID(0), 0, ""),
		logger: logger,
	}
}

func (p *StateRoomPolicy) OnStart(room realtime.Room) error {
	p.room = room
	return nil
}

func (p *StateRoomPolicy) OnStop(room realtime.Room) error {
	p.room = nil
	return nil
}

func (p *StateRoomPolicy) OnJoin(peer realtime.Peer) error {
	return nil
}

func (p *StateRoomPolicy) OnLeave(peer realtime.Peer) error {
	return nil
}

func (p *StateRoomPolicy) OnMessage(roomEvent events.RoomEvent) error {
	return nil
}
