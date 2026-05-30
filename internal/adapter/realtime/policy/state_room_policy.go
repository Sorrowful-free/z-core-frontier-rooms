package policy

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type StateRoomPolicy struct {
	room  realtime.Room
	state state.RoomState

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
	peerID := peer.GetID()
	peerState, ok := p.state.Peers[peerID]
	if !ok {
		peerState = state.PeerState{
			NickName: peer.GetNickName(),
			IsMaster: len(p.state.Peers) == 0,
			Ping:     0,
		}
		p.state.Peers[peerID] = peerState
	}
	return nil
}

func (p *StateRoomPolicy) OnLeave(peer realtime.Peer) error {
	peerID := peer.GetID()
	peerState, ok := p.state.Peers[peerID]
	if !ok {
		return fmt.Errorf("peer not found: %d", peerID)
	}
	delete(p.state.Peers, peerID)
	if peerState.IsMaster && len(p.state.Peers) > 0 {
		for id, _ := range p.state.Peers {
			peerState, ok := p.state.Peers[id]
			if !ok {
				return fmt.Errorf("peer not found: %d", id)
			}
			peerState.IsMaster = true
			p.state.Peers[id] = peerState
			break
		}
	}

	return nil
}

func (p *StateRoomPolicy) OnMessage(roomEvent events.RoomEvent) error {
	return nil
}
