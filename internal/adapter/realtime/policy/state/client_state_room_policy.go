package state

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

// OnMessage-обработчики больше не эмитят patch: они мутируют p.state
// точечно (COW-записи, без clone/diff/send), дельта доезжает клиентам
// ближайшим patch-тиком (tick-coalesced эмиссия, P34). Единственный
// регулярный эмиттер OpCodePatchState — OnTickPatchState.

func (p *StateRoomPolicy) onClientFullEntities(roomEvent events.RoomEvent) error {
	if !p.master.isMaster(roomEvent.PeerID) {
		return fmt.Errorf("%w: peer %d", domain.ErrNotMaster, roomEvent.PeerID)
	}

	full, err := p.entitiesCodec.Decode(roomEvent.Frame.Payload)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInRoomInvalidPayload, err)
	}
	p.state.Entities = full
	return nil
}

func (p *StateRoomPolicy) onClientPatchEntities(roomEvent events.RoomEvent) error {
	if !p.master.isMaster(roomEvent.PeerID) {
		return fmt.Errorf("%w: peer %d", domain.ErrNotMaster, roomEvent.PeerID)
	}

	patch, err := p.entitiesCodec.DecodePatch(roomEvent.Frame.Payload)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInRoomInvalidPayload, err)
	}

	// Двухфазный apply: staged-результаты по ключам патча (clone только
	// затронутых entity), запись в state разом — O(|patch|) и атомарность
	// «ошибка = нет побочек».
	staged := make(map[state.EntityID]state.EntityState, len(patch.Added)+len(patch.Updated))
	for id, entity := range patch.Added {
		staged[id] = entity
	}
	for id, entityPatch := range patch.Updated {
		current, ok := p.state.Entities[id]
		if !ok {
			return fmt.Errorf("%w: key %v not found in map", domain.ErrInRoomInvalidPayload, id)
		}
		next := current.Clone()
		if err := next.ApplyPatch(entityPatch); err != nil {
			return fmt.Errorf("%w: %w", domain.ErrInRoomInvalidPayload, err)
		}
		staged[id] = next
	}
	for id, entity := range staged {
		p.state.Entities[id] = entity
	}
	for _, id := range patch.Removed {
		delete(p.state.Entities, id)
	}
	return nil
}

func (p *StateRoomPolicy) onClientFullInput(roomEvent events.RoomEvent) error {
	full, err := p.inputCodec.Decode(roomEvent.Frame.Payload)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInRoomInvalidPayload, err)
	}
	peerID := roomEvent.PeerID
	if _, ok := p.state.Peers[peerID]; !ok {
		return fmt.Errorf("%w: %d", domain.ErrPeerNotFound, peerID)
	}
	p.state.Inputs[peerID] = *full
	return nil
}

func (p *StateRoomPolicy) onClientPatchInput(roomEvent events.RoomEvent) error {
	patch, err := p.inputCodec.DecodePatch(roomEvent.Frame.Payload)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInRoomInvalidPayload, err)
	}
	peerID := roomEvent.PeerID
	if _, ok := p.state.Peers[peerID]; !ok {
		return fmt.Errorf("%w: %d", domain.ErrPeerNotFound, peerID)
	}

	inputState, ok := p.state.Inputs[peerID]
	if !ok {
		inputState = state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
	} else {
		// COW: запись шарится с prevState-снапшотом — clone-then-replace.
		inputState = inputState.Clone()
	}
	if err := inputState.ApplyPatch(*patch); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInRoomInvalidPayload, err)
	}
	p.state.Inputs[peerID] = inputState
	return nil
}

func (p *StateRoomPolicy) onClientRpc(roomEvent events.RoomEvent) error {
	payload := roomEvent.Frame.Payload
	rpc, err := p.rpcCodec.Decode(payload)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInRoomInvalidPayload, err)
	}

	senderPeerID := roomEvent.PeerID
	masterID := p.master.peerID()

	peerEvent := events.PeerEvent{
		ExcludePeerID: senderPeerID,
		Frame: domain.Frame{
			OpCode:  state.OpCodeRpc,
			Payload: payload,
		},
	}

	switch rpc.Target {
	case state.RpcTargetPeer:
		if !rpc.PeerID.IsValid() {
			return domain.ErrInvalidRpcTarget
		}
		if !p.room.HasPeer(rpc.PeerID) {
			return fmt.Errorf("%w: %d", domain.ErrRpcTargetPeerNotFound, rpc.PeerID)
		}
		peerEvent.PeerID = rpc.PeerID
	case state.RpcTargetMaster:
		if !masterID.IsValid() {
			// master мог остаться nil после упавшего election - машина
			// делает lazy election по живому state (фейл логируется в
			// masterless-эпизод), затем одна повторная проверка.
			if err := p.master.dispatch(masterEvent{kind: evRpcMaster}, p.room, &p.state); err == nil {
				masterID = p.master.peerID()
			}
		}
		if !masterID.IsValid() {
			return domain.ErrNoMaster
		}
		peerEvent.PeerID = masterID
	case state.RpcTargetAll:
		peerEvent.PeerID = domain.PeerIDInvalid
	default:
		return domain.ErrInvalidRpcTarget
	}

	return p.room.Send(peerEvent)
}
