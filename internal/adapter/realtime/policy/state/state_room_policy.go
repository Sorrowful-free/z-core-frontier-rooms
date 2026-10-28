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
	// master - машина master-эпизодов: election, IsMaster-флаги и дедуп
	// masterless-лога сведены в один диспетчер переходов (master_machine.go).
	master masterMachine

	// allowMasterMigration — режим master-миграции деплоя (P33): false —
	// уход master'а убивает комнату (ErrRoomShouldClose на входе OnLeave).
	// Умолчание true задаётся в NewStateRoomPolicy; продовое значение
	// вливает StateRoomPolicyFactory полем из StatePolicyConfig.
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
		// Умолчание Noop: продовый metrics вливает StateRoomPolicyFactory
		// полем (сигнатура NewStateRoomPolicy стабильна для тестов).
		master: masterMachine{logger: logger, metrics: portmetrics.Noop()},
		// Умолчание «миграция разрешена» (историческое поведение); фабрика
		// перезаписывает полем из StatePolicyConfig, как и metrics.
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
	// No-migration режим (P33): уход master'а убивает комнату — sentinel
	// уходит наружу до входа в master-машину и staged-мутаций state
	// («ошибка = нет побочек»). Машина ещё держит уходящего master'а
	// (m.peer сбрасывается только в dispatch) — проверки по peerID
	// достаточно. Teardown (OpRoomClosed → unregister → revoke) делает
	// usecase по ErrRoomShouldClose.
	if !p.allowMasterMigration && p.master.isMaster(peer.GetID()) {
		return fmt.Errorf("%w: %d", domain.ErrRoomShouldClose, peer.GetID())
	}
	peerID := peer.GetID()
	peerState, ok := p.state.Peers[peerID]
	if !ok {
		return fmt.Errorf("%w: %d", domain.ErrPeerNotFound, peerID)
	}
	oldMaster := p.master.current()
	// Дешёвый откат без deep-clone staging: shallow-снапшот Peers +
	// запись input уходящего; при ошибке dispatch всё восстанавливается
	// разом (leave-дельта уходит patch-тиком, send-фейлов на этом пути нет).
	savedPeers := maps.Clone(p.state.Peers)
	savedInput, hasInput := p.state.Inputs[peerID]
	delete(p.state.Peers, peerID)
	delete(p.state.Inputs, peerID)
	// Последствия ухода для master решает машина: ушёл не-master -
	// перехода нет; master в пустой комнате - masterless; иначе перевыборы.
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
