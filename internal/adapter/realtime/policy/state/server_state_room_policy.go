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
	// Ping rides in the full frame: fresh values are written before
	// encode, and commitPrevState after send commits them too — the
	// patch diff neither sees nor carries ping.
	if err := p.refreshPeerPings(); err != nil {
		return fmt.Errorf("refresh peer pings: %w", err)
	}
	if err := p.sendFullState(&p.state); err != nil {
		return fmt.Errorf("send full state: %w", err)
	}
	return nil
}

// OnTickPatchState is the only regular OpCodePatchState emitter
// (tick-coalesced patch, P34): all deltas accumulated in p.state since
// the last commit (join/leave/election/client messages) go out in a
// single broadcast to everyone without exceptions — the publisher gets
// an echo of its own deltas, client-side apply is idempotent.
func (p *StateRoomPolicy) OnTickPatchState() error {
	if err := p.pruneStalePeers(); err != nil {
		return fmt.Errorf("prune stale peers: %w", err)
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

// refreshPeerPings carries live pings from the room into state. It runs
// on the full tick before sendFullState — the ping cadence is
// fullStateInterval, there is no separate mechanism. Stale records are
// skipped: their cleanup and the master transitions live in
// pruneStalePeers on the patch tick.
func (p *StateRoomPolicy) refreshPeerPings() error {
	for peerID, peerState := range p.state.Peers {
		peer, err := p.room.GetPeer(peerID)
		if err != nil {
			// stale — only when the peer is truly absent from the room;
			// other GetPeer errors are no reason to drop the peer from state.
			if errors.Is(err, domain.ErrPeerNotFound) {
				continue
			}
			return fmt.Errorf("get peer %d: %w", peerID, err)
		}
		peerState.Ping = peer.Ping()
		p.state.Peers[peerID] = peerState
	}
	return nil
}

// pruneStalePeers cleans out records of peers that are no longer in the
// room (stale is only ErrPeerNotFound) and emits evTickPrune: the
// machine decides whether a master transition is needed (the master was
// pruned or is absent while peers are alive). Bound to the patch tick —
// masterless recovery keeps the patchStateInterval cadence, not
// fullStateInterval.
func (p *StateRoomPolicy) pruneStalePeers() error {
	var stale []domain.PeerID
	for peerID := range p.state.Peers {
		_, err := p.room.GetPeer(peerID)
		if err != nil {
			if errors.Is(err, domain.ErrPeerNotFound) {
				stale = append(stale, peerID)
				continue
			}
			return fmt.Errorf("get peer %d: %w", peerID, err)
		}
	}

	// First clean out all stale records, then run the master re-election
	// once — otherwise the election could land on a stale peer not yet
	// cleaned.
	masterPruned := false
	for _, peerID := range stale {
		p.logger.Warn("pruning stale peer from room state", "peerID", peerID)
		delete(p.state.Peers, peerID)
		delete(p.state.Inputs, peerID)
		if p.master.isMaster(peerID) {
			masterPruned = true
		}
	}
	// The tick-prune event: the machine decides whether a master
	// transition is needed (pruned or absent while peers are alive) and
	// keeps the masterless-episode log.
	return p.master.dispatch(masterEvent{kind: evTickPrune, masterPruned: masterPruned}, p.room, &p.state)
}

func (p *StateRoomPolicy) joinPeerAndSyncState(peer realtime.Peer) error {
	peerID := peer.GetID()
	// Encode-only view: the machine writes the peer record into a
	// shallow snapshot (its Peers is a separate map); p.state is not
	// touched until the commit point.
	view := p.state.Snapshot()

	oldMaster := p.master.current()
	committed := false
	// The join event may reassign the master before the commit point
	// (the first peer of an empty room) — we roll back on all error
	// branches.
	defer func() {
		if !committed {
			p.master.restore(oldMaster)
		}
	}()

	// The machine writes the peer record into the staged state and
	// decides its IsMaster flag.
	if err := p.master.dispatch(masterEvent{kind: evPeerJoined, peer: peer}, p.room, view); err != nil {
		return err
	}

	if err := p.deliverFullStateToPeer(peer, view); err != nil {
		return fmt.Errorf("send full state to peer %d: %w", peerID, err)
	}

	// Commit — a point write into p.state; the join delta reaches the
	// others on the next patch tick (prevState is not committed here).
	p.state.Peers[peerID] = view.Peers[peerID]
	committed = true
	// Post-commit: join may have moved masterless -> masterSet and
	// closed the episode.
	p.master.assigned()
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

// deliverFullStateToPeer sends the full state directly to the peer: at
// OnJoin time the peer is not yet inserted into r.peers, so room.Send by
// peerID does not find it. The only immediate emission outside the
// ticks — the joiner cannot wait for the full tick.
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

// sendPatchState — broadcast of the patch to everyone without
// exceptions (universal broadcast, P34). prevState is committed only
// after a successful send — a failed broadcast does not lose the delta,
// it goes out on the next tick.
func (p *StateRoomPolicy) sendPatchState(patch *state.RoomStatePatch) error {
	payload, err := p.stateCodec.EncodePatch(patch)
	if err != nil {
		return err
	}
	if err := p.room.Send(events.PeerEvent{
		Frame: domain.Frame{
			OpCode:  state.OpCodePatchState,
			Payload: payload,
		},
	}); err != nil {
		return err
	}
	p.commitPrevState()
	return nil
}

// commitPrevState — prevState commit points: after successful
// sendFullState / sendPatchState. The snapshot is shallow (values are
// shared with state) — correct under the RoomState COW invariant and
// does not pay a deep clone every tick.
func (p *StateRoomPolicy) commitPrevState() {
	p.prevState = *p.state.Snapshot()
}
