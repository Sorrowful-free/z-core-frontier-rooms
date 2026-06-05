package state

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
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

	fullStateTicker  *time.Ticker
	patchStateTicker *time.Ticker

	ctx    context.Context
	cancel context.CancelFunc

	timersWg sync.WaitGroup

	logger logging.Logger
}

func NewStateRoomPolicy(logger logging.Logger, stateCodec codec.RoomStateCodec, entitiesCodec codec.EntitiesStateCodec, inputCodec codec.InputStateCodec, rpcCodec codec.RpcStateCodec, fullStateInterval time.Duration, patchStateInterval time.Duration) *StateRoomPolicy {
	return &StateRoomPolicy{
		prevState: *state.NewRoomState(domain.RoomID(0), 0, ""),
		state:     *state.NewRoomState(domain.RoomID(0), 0, ""),

		stateCodec:      stateCodec,
		entitiesCodec:   entitiesCodec,
		inputCodec:      inputCodec,
		rpcCodec:        rpcCodec,
		fullStateTicker:  time.NewTicker(fullStateInterval),
		patchStateTicker: time.NewTicker(patchStateInterval),

		logger: logger,
	}
}

func (p *StateRoomPolicy) OnStart(room realtime.Room) error {
	p.room = room
	p.ctx, p.cancel = context.WithCancel(room.Context())
	p.processRoomTimers()
	return nil
}

func (p *StateRoomPolicy) OnStop(room realtime.Room) error {
	if p.cancel != nil {
		p.cancel()
	}
	p.fullStateTicker.Stop()
	p.patchStateTicker.Stop()
	p.timersWg.Wait()
	p.room = nil
	return nil
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
		if peerState.IsMaster && len(roomState.Peers) > 0 {
			for id, _ := range roomState.Peers {
				peerState, ok := roomState.Peers[id]
				if !ok {
					return fmt.Errorf("peer not found: %d", id)
				}
				newPeer, err := p.room.GetPeer(id)
				if err != nil {
					return err
				}
				peerState.IsMaster = true
				roomState.Peers[id] = peerState
				p.master = newPeer
				break
			}
		}
		return nil
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

	case tickOpCodeFullState:
		return p.onTickFullState()
	case tickOpCodePatchState:
		return p.onTickPatchState()
	}

	return nil
}
