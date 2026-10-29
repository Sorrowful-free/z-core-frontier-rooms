package state

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

// OnMessage handlers no longer emit a patch: they mutate p.state
// point-wise (COW writes, no clone/diff/send), and the delta reaches
// clients on the next patch tick (tick-coalesced patch, P34). The only
// regular OpCodePatchState emitter is OnTickPatchState.

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

	// Two-phase apply: staged results per patch key (clone only the
	// touched entities), written into state at once — O(|patch|) and
	// "error = no side effects" atomicity.
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
		// COW: the record is shared with the prevState snapshot — clone-then-replace.
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
	if _, ok := p.state.Peers[senderPeerID]; !ok {
		return fmt.Errorf("%w: %d", domain.ErrPeerNotFound, senderPeerID)
	}
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
			// master may still be nil after a failed election — the
			// machine runs a lazy election against the live state (a
			// failure is logged into the masterless episode), then one
			// re-check.
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
