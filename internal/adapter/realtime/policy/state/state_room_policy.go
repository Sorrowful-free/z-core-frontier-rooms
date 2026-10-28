package state

import (
	"fmt"
	"maps"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
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
	room realtime.Room
	// master — the master-episode machine: election, IsMaster flags and
	// the masterless log dedup are gathered into one transition
	// dispatcher (master_machine.go).
	master masterMachine

	// allowMasterMigration — the deploy's master-migration mode (P33):
	// false — the master's departure kills the room (ErrRoomShouldClose
	// at the OnLeave entry). The true default is set in
	// NewStateRoomPolicy; the production value is injected by
	// StateRoomPolicyFactory via the field from StatePolicyConfig.
	allowMasterMigration bool

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
		// Noop default: the production metrics are injected by
		// StateRoomPolicyFactory via the field (the NewStateRoomPolicy
		// signature stays stable for tests).
		master: masterMachine{logger: logger, metrics: portmetrics.Noop()},
		// "Migration allowed" default (historical behavior); the factory
		// overwrites it via the field from StatePolicyConfig, same as
		// metrics.
		allowMasterMigration: true,
		prevState:            *state.NewRoomState(domain.RoomIDInvalid, 0),
		state:                *state.NewRoomState(domain.RoomIDInvalid, 0),

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
	rs := state.NewRoomState(room.GetID(), room.GetCapacity())
	p.state = *rs
	p.prevState = *rs.Snapshot()
}

func (p *StateRoomPolicy) OnJoin(peer realtime.Peer) error {
	return p.joinPeerAndSyncState(peer)
}

func (p *StateRoomPolicy) OnLeave(peer realtime.Peer) error {
	// No-migration mode (P33): the master's departure kills the room —
	// the sentinel goes outward before entering the master machine and
	// the staged state mutations ("error = no side effects"). The
	// machine still holds the departing master (m.peer is reset only in
	// dispatch) — a peerID check suffices. The usecase performs teardown
	// (OpRoomClosed → unregister → revoke) on ErrRoomShouldClose.
	if !p.allowMasterMigration && p.master.isMaster(peer.GetID()) {
		return fmt.Errorf("%w: %d", domain.ErrRoomShouldClose, peer.GetID())
	}
	peerID := peer.GetID()
	peerState, ok := p.state.Peers[peerID]
	if !ok {
		return fmt.Errorf("%w: %d", domain.ErrPeerNotFound, peerID)
	}
	oldMaster := p.master.current()
	// Cheap rollback without deep-clone staging: a shallow snapshot of
	// Peers plus the leaver's input record; on a dispatch error
	// everything is restored at once (the leave delta goes out on the
	// patch tick, there are no send failures on this path).
	savedPeers := maps.Clone(p.state.Peers)
	savedInput, hasInput := p.state.Inputs[peerID]
	delete(p.state.Peers, peerID)
	delete(p.state.Inputs, peerID)
	// The machine decides the master's fate on leave: a non-master left —
	// no transition; the master left an empty room — masterless;
	// otherwise re-election.
	if err := p.master.dispatch(masterEvent{kind: evPeerLeft, leftIsMaster: peerState.IsMaster}, p.room, &p.state); err != nil {
		p.state.Peers = savedPeers
		if hasInput {
			p.state.Inputs[peerID] = savedInput
		}
		p.master.restore(oldMaster)
		return err
	}
	p.master.assigned()
	return nil
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
