package state

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func (p *StateRoomPolicy) onClientFullEntities(roomEvent events.RoomEvent) error {
	if p.master == nil || roomEvent.PeerID != p.master.GetID() {
		return fmt.Errorf("%w: peer %d", domain.ErrNotMaster, roomEvent.PeerID)
	}

	oldRoomState := p.state.Clone()
	full, err := p.entitiesCodec.Decode(roomEvent.Frame.Payload)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInRoomInvalidPayload, err)
	}
	p.state.Entities = full
	return p.publishRoomStateChange(oldRoomState, p.masterPeerID())
}

func (p *StateRoomPolicy) onClientPatchEntities(roomEvent events.RoomEvent) error {
	if p.master == nil || roomEvent.PeerID != p.master.GetID() {
		return fmt.Errorf("%w: peer %d", domain.ErrNotMaster, roomEvent.PeerID)
	}

	oldRoomState := p.state.Clone()
	patch, err := p.entitiesCodec.DecodePatch(roomEvent.Frame.Payload)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInRoomInvalidPayload, err)
	}
	nextEntities, err := state.ApplyMapStatePatchCopy(
		p.state.Entities,
		*patch,
		func(e state.EntityState) state.EntityState { return e.Clone() },
		func(e1 state.EntityState, e2 state.EntityStatePatch) (*state.EntityState, error) {
			if err := e1.ApplyPatch(e2); err != nil {
				return nil, err
			}
			return &e1, nil
		},
	)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInRoomInvalidPayload, err)
	}
	p.state.Entities = nextEntities
	return p.publishRoomStateChange(oldRoomState, p.masterPeerID())
}

func (p *StateRoomPolicy) onClientFullInput(roomEvent events.RoomEvent) error {
	full, err := p.inputCodec.Decode(roomEvent.Frame.Payload)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInRoomInvalidPayload, err)
	}

	return p.prepareAndSendRoomStatePatch(func(roomState *state.RoomState) error {
		roomState.Inputs[roomEvent.PeerID] = *full
		return nil
	})
}

func (p *StateRoomPolicy) onClientPatchInput(roomEvent events.RoomEvent) error {
	patch, err := p.inputCodec.DecodePatch(roomEvent.Frame.Payload)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInRoomInvalidPayload, err)
	}
	peerID := roomEvent.PeerID

	return p.prepareAndSendRoomStatePatch(func(roomState *state.RoomState) error {
		inputState, ok := roomState.Inputs[peerID]
		if !ok {
			inputState = state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
		} else {
			inputState = inputState.Clone()
		}
		if err := inputState.ApplyPatch(*patch); err != nil {
			return fmt.Errorf("%w: %w", domain.ErrInRoomInvalidPayload, err)
		}
		roomState.Inputs[peerID] = inputState
		return nil
	})
}

func (p *StateRoomPolicy) onClientRpc(roomEvent events.RoomEvent) error {
	payload := roomEvent.Frame.Payload
	rpc, err := p.rpcCodec.Decode(payload)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInRoomInvalidPayload, err)
	}

	senderPeerID := roomEvent.PeerID
	masterID := p.masterPeerID()

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
