package state

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func (p *StateRoomPolicy) onClientFullEntities(roomEvent events.RoomEvent) error {
	if p.master == nil || roomEvent.PeerID != p.master.GetID() {
		return fmt.Errorf("peer not master: %d", roomEvent.PeerID)
	}

	oldRoomState := p.state.Clone()
	full, err := p.entitiesCodec.Decode(roomEvent.Frame.Payload)
	if err != nil {
		return err
	}
	p.state.Entities = full
	return p.publishRoomStateChange(oldRoomState, p.masterPeerID())
}

func (p *StateRoomPolicy) onClientPatchEntities(roomEvent events.RoomEvent) error {
	if p.master == nil || roomEvent.PeerID != p.master.GetID() {
		return fmt.Errorf("peer not master: %d", roomEvent.PeerID)
	}

	oldRoomState := p.state.Clone()
	patch, err := p.entitiesCodec.DecodePatch(roomEvent.Frame.Payload)
	if err != nil {
		return err
	}
	if err := state.ApplyMapStatePatch(p.state.Entities, *patch, func(e1 state.EntityState, e2 state.EntityStatePatch) (*state.EntityState, error) {
		if err := e1.ApplyPatch(e2); err != nil {
			return nil, err
		}
		return &e1, nil
	}); err != nil {
		return err
	}
	return p.publishRoomStateChange(oldRoomState, p.masterPeerID())
}

func (p *StateRoomPolicy) onClientFullInput(roomEvent events.RoomEvent) error {
	full, err := p.inputCodec.Decode(roomEvent.Frame.Payload)
	if err != nil {
		return err
	}

	return p.prepareAndSendRoomStatePatch(func(roomState *state.RoomState) error {
		roomState.Inputs[roomEvent.PeerID] = *full
		return nil
	})
}

func (p *StateRoomPolicy) onClientPatchInput(roomEvent events.RoomEvent) error {
	patch, err := p.inputCodec.DecodePatch(roomEvent.Frame.Payload)
	if err != nil {
		return err
	}
	peerID := roomEvent.PeerID

	return p.prepareAndSendRoomStatePatch(func(roomState *state.RoomState) error {
		inputState, ok := roomState.Inputs[peerID]
		if !ok {
			inputState = state.InputState{}
		}
		err := inputState.ApplyPatch(*patch)
		if err != nil {
			return err
		}
		roomState.Inputs[peerID] = inputState
		return nil
	})
}

func (p *StateRoomPolicy) onClientRpc(roomEvent events.RoomEvent) error {
	payload := roomEvent.Frame.Payload
	rpc, err := p.rpcCodec.Decode(payload)
	if err != nil {
		return err
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
		if rpc.PeerID.IsValid() && (!masterID.IsValid() || rpc.PeerID != masterID) {
			peerEvent.PeerID = rpc.PeerID
		}
	case state.RpcTargetMaster:
		if masterID.IsValid() && rpc.PeerID == masterID {
			peerEvent.PeerID = rpc.PeerID
		}
	case state.RpcTargetAll:
		peerEvent.PeerID = domain.PeerIDInvalid
	}

	return p.room.Send(peerEvent)
}
