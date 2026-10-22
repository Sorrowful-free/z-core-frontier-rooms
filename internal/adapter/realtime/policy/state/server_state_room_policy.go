package state

import (
	"errors"
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

func (p *StateRoomPolicy) OnTickFullState() error {
	if err := p.sendFullState(&p.state); err != nil {
		return fmt.Errorf("send full state: %w", err)
	}
	return nil
}

func (p *StateRoomPolicy) OnTickPatchState() error {
	if err := p.refreshPeerPings(); err != nil {
		return fmt.Errorf("refresh peer pings: %w", err)
	}
	patch, err := p.prevState.MakePatch(&p.state)
	if err != nil {
		return fmt.Errorf("make patch state: %w", err)
	}
	if patch == nil {
		return nil
	}
	return p.sendPatchState(patch)
}

func (p *StateRoomPolicy) refreshPeerPings() error {
	var stale []domain.PeerID
	for peerID, peerState := range p.state.Peers {
		peer, err := p.room.GetPeer(peerID)
		if err != nil {
			// stale — только когда peer реально отсутствует в room;
			// прочие ошибки GetPeer не повод выкидывать peer из state.
			if errors.Is(err, domain.ErrPeerNotFound) {
				stale = append(stale, peerID)
				continue
			}
			return fmt.Errorf("get peer %d: %w", peerID, err)
		}
		peerState.Ping = peer.Ping()
		p.state.Peers[peerID] = peerState
	}

	// Сначала вычищаем все stale-записи, потом один раз переизбираем master —
	// иначе выборы могли бы попасть на ещё не вычищенный stale peer.
	masterPruned := false
	for _, peerID := range stale {
		p.logger.Warn("pruning stale peer from room state", "peerID", peerID)
		delete(p.state.Peers, peerID)
		delete(p.state.Inputs, peerID)
		if p.master != nil && p.master.GetID() == peerID {
			masterPruned = true
		}
	}
	if masterPruned {
		if err := p.electAndAssignMaster(&p.state); err != nil {
			// master не должен висеть на удалённом peer — сбрасываем.
			p.logger.Error("elect master after pruning stale peers failed", "error", err)
			p.master = nil
		}
	}
	return nil
}

func (p *StateRoomPolicy) masterPeerID() domain.PeerID {
	if p.master == nil {
		return domain.PeerIDInvalid
	}
	return p.master.GetID()
}

func (p *StateRoomPolicy) joinPeerAndSyncState(peer realtime.Peer) error {
	peerID := peer.GetID()
	newState := p.state.Clone()
	newMaster := p.master

	if _, ok := newState.Peers[peerID]; !ok {
		if len(newState.Peers) == 0 {
			newMaster = peer
		}
		newState.Peers[peerID] = state.PeerState{
			NickName: peer.GetNickName(),
			IsMaster: len(newState.Peers) == 0,
			Ping:     peer.Ping(),
		}
	}

	if err := p.deliverFullStateToPeer(peer, newState); err != nil {
		return fmt.Errorf("send full state to peer %d: %w", peerID, err)
	}

	roomPatch, err := p.state.MakePatch(newState)
	if err != nil {
		return fmt.Errorf("make patch on join: %w", err)
	}
	if roomPatch != nil {
		if err := p.sendPatchStateExclude(peerID, roomPatch); err != nil {
			return fmt.Errorf("send patch state on join: %w", err)
		}
	}

	p.state = *newState
	p.master = newMaster
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
	}
	return p.room.Send(peerEvent)
}

// deliverFullStateToPeer шлёт full state напрямую в peer: на момент OnJoin
// peer ещё не вставлен в r.peers, поэтому room.Send по peerID не находит его.
func (p *StateRoomPolicy) deliverFullStateToPeer(peer realtime.Peer, full *state.RoomState) error {
	payload, err := p.stateCodec.Encode(full)
	if err != nil {
		return err
	}
	return peer.Deliver(events.PeerEvent{
		PeerID: peer.GetID(),
		Frame: domain.Frame{
			OpCode:  state.OpCodeFullState,
			Payload: payload,
		},
	})
}

func (p *StateRoomPolicy) publishRoomStateChange(old *state.RoomState, exclude domain.PeerID) error {
	roomPatch, err := old.MakePatch(&p.state)
	if err != nil {
		return err
	}
	if roomPatch == nil {
		return nil
	}
	if err := p.sendPatchStateExclude(exclude, roomPatch); err != nil {
		return err
	}
	p.commitPrevState()
	return nil
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
	oldMaster := p.master
	committed := false
	// applyChanges может переназначить p.master — откатываем на всех ветках ошибок.
	defer func() {
		if !committed {
			p.master = oldMaster
		}
	}()

	newRoomState := p.state.Clone()
	if err := applyChanges(newRoomState); err != nil {
		return err
	}
	roomPatch, err := p.state.MakePatch(newRoomState)
	if err != nil {
		return err
	}
	if roomPatch == nil {
		return nil
	}
	if err := p.sendPatchStateExclude(domain.PeerIDInvalid, roomPatch); err != nil {
		return err
	}
	p.state = *newRoomState
	p.commitPrevState()
	committed = true
	return nil
}
