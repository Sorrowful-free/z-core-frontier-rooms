package state

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

// Внутренние opcodes: только room.Deliver из таймеров policy, на wire клиентам не уходят.
const (
	tickOpCodeFullState  domain.OpCode = 0xF0
	tickOpCodePatchState domain.OpCode = 0xF1
)

func (p *StateRoomPolicy) processRoomTimers() error {
	p.timersWg.Add(1)
	go func() {
		defer p.timersWg.Done()
		for {
			select {
			case <-p.ctx.Done():
				return
			case <-p.fullStateTicker.C:
				if err := p.deliverTick(tickOpCodeFullState); err != nil {
					p.logger.Error("failed to deliver full state tick", "error", err)
				}
			case <-p.patchStateTicker.C:
				if err := p.deliverTick(tickOpCodePatchState); err != nil {
					p.logger.Error("failed to deliver patch state tick", "error", err)
				}
			}
		}
	}()
	return nil
}

func (p *StateRoomPolicy) deliverTick(op domain.OpCode) error {
	if p.room == nil || p.ctx.Err() != nil {
		return nil
	}
	if err := p.room.Context().Err(); err != nil {
		return nil
	}
	return p.room.Deliver(events.RoomEvent{
		Frame: domain.Frame{OpCode: op},
	})
}

func (p *StateRoomPolicy) onTickFullState() error {
	if err := p.sendFullState(&p.state); err != nil {
		return fmt.Errorf("send full state: %w", err)
	}
	return nil
}

func (p *StateRoomPolicy) onTickPatchState() error {
	patch, err := p.prevState.MakePatch(&p.state)
	if err != nil {
		return fmt.Errorf("make patch state: %w", err)
	}
	if patch == nil {
		return nil
	}
	return p.sendPatchState(patch)
}

func (p *StateRoomPolicy) masterPeerID() domain.PeerID {
	if p.master == nil {
		return domain.PeerIDInvalid
	}
	return p.master.GetID()
}

func (p *StateRoomPolicy) joinPeerAndSyncState(peer realtime.Peer) error {
	peerID := peer.GetID()
	oldRoomState := p.state.Clone()

	if _, ok := p.state.Peers[peerID]; !ok {
		if len(p.state.Peers) == 0 {
			p.master = peer
		}
		p.state.Peers[peerID] = state.PeerState{
			NickName: peer.GetNickName(),
			IsMaster: len(p.state.Peers) == 0,
			Ping:     0,
		}
	}

	if err := p.sendFullStateToPeer(peerID, &p.state); err != nil {
		return fmt.Errorf("send full state to peer %d: %w", peerID, err)
	}

	roomPatch, err := oldRoomState.MakePatch(&p.state)
	if err != nil {
		return err
	}
	if roomPatch != nil {
		if err := p.sendPatchStateExclude(peerID, roomPatch); err != nil {
			return fmt.Errorf("send patch state on join: %w", err)
		}
	}
	p.commitPrevState()
	return nil
}

func (p *StateRoomPolicy) sendFullState(full *state.RoomState) error {
	if err := p.sendFullStateToPeer(domain.PeerIDInvalid, full); err != nil {
		return err
	}
	p.commitPrevState()
	return nil
}

func (p *StateRoomPolicy) sendFullStateToPeer(peerID domain.PeerID, full *state.RoomState) error {
	payload, err := p.stateCodec.Encode(full)
	if err != nil {
		return err
	}
	peerEvent := events.PeerEvent{
		Frame: domain.Frame{
			OpCode:  state.OpCodeFullState,
			Payload: payload,
		},
	}
	if peerID.IsValid() {
		peerEvent.PeerID = peerID
	} else {
		peerEvent.ExcludePeerID = p.masterPeerID()
	}
	return p.room.Send(peerEvent)
}

func (p *StateRoomPolicy) sendPatchState(patch *state.RoomStatePatch) error {
	if err := p.sendPatchStateExclude(domain.PeerIDInvalid, patch); err != nil {
		return err
	}
	p.commitPrevState()
	return nil
}

func (p *StateRoomPolicy) sendPatchStateExclude(exclude domain.PeerID, patch *state.RoomStatePatch) error {
	payload, err := p.stateCodec.EncodePatch(patch)
	if err != nil {
		return err
	}
	peerEvent := events.PeerEvent{
		Frame: domain.Frame{
			OpCode:  state.OpCodePatchState,
			Payload: payload,
		},
	}
	if exclude.IsValid() {
		peerEvent.ExcludePeerID = exclude
	}
	return p.room.Send(peerEvent)
}

func (p *StateRoomPolicy) commitPrevState() {
	p.prevState = *p.state.Clone()
}

func (p *StateRoomPolicy) prepareAndSendRoomStatePatch(applyChanges func(roomState *state.RoomState) error) error {
	oldRoomState := p.state.Clone()
	err := applyChanges(&p.state)
	if err != nil {
		return err
	}
	roomPatch, err := oldRoomState.MakePatch(&p.state)
	if err != nil {
		return err
	}
	if roomPatch == nil {
		return nil
	}
	return p.sendPatchState(roomPatch)
}
