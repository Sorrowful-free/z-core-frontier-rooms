package state

import (
	"fmt"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/codec"
	portpolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
)

var _ portpolicy.RoomPolicy = (*StateRoomPolicy)(nil)

const (
	DefaultFullStateInterval  = 4 * time.Second
	DefaultPatchStateInterval = time.Second / 20
)

type StateRoomPolicy struct {
	room   realtime.Room
	master realtime.Peer

	prevState state.RoomState
	state     state.RoomState

	stateCodec    codec.RoomStateCodec
	entitiesCodec codec.EntitiesStateCodec
	inputCodec    codec.InputStateCodec
	rpcCodec      codec.RpcStateCodec

	fullStateInterval  time.Duration
	patchStateInterval time.Duration

	logger logging.Logger
}

func normalizeTickInterval(interval, defaultInterval time.Duration) time.Duration {
	if interval <= 0 {
		return defaultInterval
	}
	return interval
}

func NewStateRoomPolicy(logger logging.Logger, stateCodec codec.RoomStateCodec, entitiesCodec codec.EntitiesStateCodec, inputCodec codec.InputStateCodec, rpcCodec codec.RpcStateCodec, fullStateInterval time.Duration, patchStateInterval time.Duration) *StateRoomPolicy {
	return &StateRoomPolicy{
		prevState: *state.NewRoomState(domain.RoomID(0), 0, ""),
		state:     *state.NewRoomState(domain.RoomID(0), 0, ""),

		stateCodec:         stateCodec,
		entitiesCodec:      entitiesCodec,
		inputCodec:         inputCodec,
		rpcCodec:           rpcCodec,
		fullStateInterval:  normalizeTickInterval(fullStateInterval, DefaultFullStateInterval),
		patchStateInterval: normalizeTickInterval(patchStateInterval, DefaultPatchStateInterval),

		logger: logger,
	}
}

func (p *StateRoomPolicy) OnStart(room realtime.Room) error {
	p.room = room
	p.syncRoomStateFromRoom(room)
	return nil
}

func (p *StateRoomPolicy) OnStop(room realtime.Room) error {
	p.room = nil
	return nil
}

func (p *StateRoomPolicy) TickIntervals() (time.Duration, time.Duration) {
	return p.fullStateInterval, p.patchStateInterval
}

func (p *StateRoomPolicy) syncRoomStateFromRoom(room realtime.Room) {
	rs := state.NewRoomState(room.GetID(), room.GetCapacity(), "")
	p.state = *rs
	p.prevState = *rs.Clone()
}

func (p *StateRoomPolicy) OnJoin(peer realtime.Peer) error {
	return p.joinPeerAndSyncState(peer)
}

func (p *StateRoomPolicy) OnLeave(peer realtime.Peer) error {
	return p.prepareAndSendRoomStatePatch(func(roomState *state.RoomState) error {
		peerID := peer.GetID()
		peerState, ok := roomState.Peers[peerID]
		if !ok {
			return fmt.Errorf("peer not found: %d", peerID)
		}
		delete(roomState.Peers, peerID)
		delete(roomState.Inputs, peerID)
		if !peerState.IsMaster {
			return nil
		}
		if len(roomState.Peers) == 0 {
			p.master = nil
			return nil
		}
		return p.electAndAssignMaster(roomState)
	})
}

func (p *StateRoomPolicy) OnMessage(roomEvent events.RoomEvent) error {

	opcode := roomEvent.Frame.OpCode
	switch opcode {
	case state.OpCodeFullEntities:
		return p.onClientFullEntities(roomEvent)
	case state.OpCodePatchEntities:
		return p.onClientPatchEntities(roomEvent)

	case state.OpCodeFullInput:
		return p.onClientFullInput(roomEvent)
	case state.OpCodePatchInput:
		return p.onClientPatchInput(roomEvent)
	case state.OpCodeRpc:
		return p.onClientRpc(roomEvent)
	default:
		return fmt.Errorf("%w: %#x", domain.ErrInRoomUnknownOpcode, opcode)
	}
}
