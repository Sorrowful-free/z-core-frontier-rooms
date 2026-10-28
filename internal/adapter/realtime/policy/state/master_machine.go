package state

import (
	"errors"
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

// masterMachine — the master-episode machine of StateRoomPolicy.
//
// States are derived from peer + masterlessLogged:
//   - masterSet: peer != nil (a master is elected, its record holds
//     IsMaster);
//   - masterless: peer == nil; masterlessLogged=true means an open
//     episode (Error already logged, repeated re-elect failures do not
//     spam).
//
// The triggers (join/leave/tick/rpc) do not know the election ritual:
// they emit masterEvent and dispatch decides the transition — election,
// the roomState IsMaster flags and the masterless-episode log live in
// one place. Transitions executed inside stage-then-commit (the join
// snapshot, the manual OnLeave rollback) mutate m.peer before the
// commit point; the transaction owner rolls it back via restore on
// error branches.
type masterMachine struct {
	peer             realtime.Peer
	masterlessLogged bool
	logger           logging.Logger
	metrics          portmetrics.Metrics
}

// masterEventKind — trigger events capable of changing the master.
type masterEventKind int

const (
	// evPeerJoined — OnJoin: the machine decides whether the peer
	// becomes master (only the first peer of an empty room) and writes
	// its record into roomState.
	evPeerJoined masterEventKind = iota
	// evPeerLeft — OnLeave: the leaver's record is already removed from
	// roomState; a transition is needed only if it held IsMaster.
	evPeerLeft
	// evTickPrune — OnTickPatchState: the stale prune may have cleaned
	// out the master, plus the masterless recovery window while peers
	// are alive.
	evTickPrune
	// evRpcMaster — OnMessage RPC->master while masterless: lazy
	// election.
	evRpcMaster
)

// masterEvent — a trigger event; only the fields of the relevant kind are filled.
type masterEvent struct {
	kind         masterEventKind
	peer         realtime.Peer // evPeerJoined: the joined peer
	leftIsMaster bool          // evPeerLeft: the leaver held IsMaster
	masterPruned bool          // evTickPrune: the master hit the stale cleanup
}

// dispatch — the single transition dispatcher. Mutates roomState (the
// joined peer's record, the IsMaster flags) and m.peer. Only evPeerLeft
// returns an error: an election failure on leave fails Leave entirely;
// the machine swallows tick/rpc transition errors into the episode log.
func (m *masterMachine) dispatch(ev masterEvent, room realtime.Room, roomState *state.RoomState) error {
	switch ev.kind {
	case evPeerJoined:
		return m.onPeerJoined(ev.peer, roomState)
	case evPeerLeft:
		return m.onPeerLeft(ev.leftIsMaster, room, roomState)
	case evTickPrune:
		m.onTickPrune(ev.masterPruned, room, roomState)
	case evRpcMaster:
		m.onRpcMaster(room, roomState)
	}
	return nil
}

// onPeerJoined: masterless -> masterSet only for the room's first peer;
// in a non-empty room join does not touch the master. The transition is
// staged: m.peer is assigned before the commit point, the caller rolls
// back via restore.
func (m *masterMachine) onPeerJoined(peer realtime.Peer, roomState *state.RoomState) error {
	if _, ok := roomState.Peers[peer.GetID()]; ok {
		return nil
	}
	first := len(roomState.Peers) == 0
	roomState.Peers[peer.GetID()] = state.PeerState{
		NickName: peer.GetNickName(),
		IsMaster: first,
		Ping:     peer.Ping(),
	}
	if first {
		m.peer = peer
		// The first election is the same "master assignment" as assign on
		// re-election: count it here, otherwise the room's first election
		// is invisible to master_elections_total. The emit is staged
		// (before the join commit point): a rolled-back join gives an
		// extra count — the same approximation as the staged emit in
		// assign.
		m.metrics.MasterElected()
	}
	return nil
}

// onPeerLeft: masterSet -> masterSet via re-election, or -> masterless
// in an emptied room. An election error goes outward (Leave fails);
// the caller's transaction rolls back m.peer.
func (m *masterMachine) onPeerLeft(leftIsMaster bool, room realtime.Room, roomState *state.RoomState) error {
	if !leftIsMaster {
		return nil
	}
	if len(roomState.Peers) == 0 {
		m.peer = nil
		return nil
	}
	return m.electAndAssign(room, roomState)
}

// onTickPrune: masterSet -> masterless if the master was cleaned out by
// the stale prune, or masterless -> masterSet recovery while peers are
// alive. An election failure does not go outward: peer is reset, the
// episode is logged.
func (m *masterMachine) onTickPrune(masterPruned bool, room realtime.Room, roomState *state.RoomState) {
	// Re-elect if the master was cleaned out together with the stale
	// records or is absent entirely (the previous election attempt
	// failed) — as long as there are live peers.
	if !masterPruned && (m.peer != nil || len(roomState.Peers) == 0) {
		return
	}
	if err := m.electAndAssign(room, roomState); err != nil {
		// master must not hang on a removed peer — reset it.
		m.peer = nil
		m.logMasterless(err)
		return
	}
	m.assigned()
}

// onRpcMaster: lazy election against the live state while masterless;
// a failure is logged into the episode, only the refreshed peerID()
// goes outward.
func (m *masterMachine) onRpcMaster(room realtime.Room, roomState *state.RoomState) {
	if m.peer != nil {
		return
	}
	if err := m.electAndAssign(room, roomState); err != nil {
		m.logMasterless(err)
		return
	}
	m.assigned()
}

// electAndAssign — the "re-election" transition: the candidate comes
// from electMasterPeerID, then a validate-first assign. Mutates the
// roomState IsMaster flags and m.peer.
func (m *masterMachine) electAndAssign(room realtime.Room, roomState *state.RoomState) error {
	masterID, err := electMasterPeerID(room, roomState.Peers)
	if err != nil {
		return err
	}
	return m.assign(room, roomState, masterID)
}

// electMasterPeerID elects a new master among the peers.
// Among peers with Ping() >= 0 — the lowest ping; on ties — the lowest
// PeerID. If nobody has an initialized ping (all -1) — the lowest
// PeerID. Stale records (peer in state but not in the room —
// ErrPeerNotFound) are skipped: the election runs among the live. If
// all records are stale — PeerIDInvalid (an empty election, not an
// error). Other GetPeer errors fail the election entirely.
func electMasterPeerID(room realtime.Room, peers state.MapState[domain.PeerID, state.PeerState]) (domain.PeerID, error) {
	if len(peers) == 0 {
		return domain.PeerIDInvalid, nil
	}

	type candidate struct {
		id   domain.PeerID
		ping int64
	}

	validPing := make([]candidate, 0, len(peers))
	allIDs := make([]domain.PeerID, 0, len(peers))

	for id := range peers {
		peer, err := room.GetPeer(id)
		if err != nil {
			if errors.Is(err, domain.ErrPeerNotFound) {
				continue
			}
			return domain.PeerIDInvalid, fmt.Errorf("get peer %d: %w", id, err)
		}
		allIDs = append(allIDs, id)
		ping := peer.Ping()
		if ping >= 0 {
			validPing = append(validPing, candidate{id: id, ping: ping})
		}
	}

	if len(validPing) > 0 {
		best := validPing[0]
		for _, c := range validPing[1:] {
			if c.ping < best.ping || (c.ping == best.ping && c.id < best.id) {
				best = c
			}
		}
		return best.id, nil
	}

	if len(allIDs) == 0 {
		// All state.Peers records turned out stale — nobody to elect.
		return domain.PeerIDInvalid, nil
	}

	bestID := allIDs[0]
	for _, id := range allIDs[1:] {
		if id < bestID {
			bestID = id
		}
	}
	return bestID, nil
}

// assign appoints the master. Validate-first: GetPeer(masterID) runs
// before the IsMaster flags are written — on error state is untouched
// and does not advertise a master the policy does not have.
func (m *masterMachine) assign(room realtime.Room, roomState *state.RoomState, masterID domain.PeerID) error {
	if !masterID.IsValid() {
		m.peer = nil
		// Empty election — no master; no flags may remain on anyone.
		for id, peerState := range roomState.Peers {
			if peerState.IsMaster {
				peerState.IsMaster = false
				roomState.Peers[id] = peerState
			}
		}
		return nil
	}

	newMaster, err := room.GetPeer(masterID)
	if err != nil {
		return fmt.Errorf("get elected master %d: %w", masterID, err)
	}

	for id, peerState := range roomState.Peers {
		wantMaster := id == masterID
		if peerState.IsMaster != wantMaster {
			peerState.IsMaster = wantMaster
			roomState.Peers[id] = peerState
		}
	}

	m.peer = newMaster
	m.metrics.MasterElected()
	return nil
}

// logMasterless — Error once per masterless episode: tick re-elect and
// lazy RPC re-elect share one flag, repeated failures do not spam.
func (m *masterMachine) logMasterless(err error) {
	if m.masterlessLogged {
		return
	}
	m.logger.Error("elect master failed, room is masterless", "error", err)
	m.masterlessLogged = true
}

// assigned closes the masterless episode (if there was one): an Info
// about recovery and a reset of the dedup flag. Triggers call it only
// at commit points (after the new master is fixed: tick, patch send,
// join, lazy RPC), otherwise a rolled-back transition would give a
// false "episode over".
func (m *masterMachine) assigned() {
	if m.peer == nil || !m.masterlessLogged {
		return
	}
	m.logger.Info("master elected, masterless episode over", "peerID", m.peer.GetID())
	m.masterlessLogged = false
}

// peerID — the current master or PeerIDInvalid while masterless.
func (m *masterMachine) peerID() domain.PeerID {
	if m.peer == nil {
		return domain.PeerIDInvalid
	}
	return m.peer.GetID()
}

// isMaster — true if peerID is the policy's current master.
func (m *masterMachine) isMaster(peerID domain.PeerID) bool {
	return m.peer != nil && m.peer.GetID() == peerID
}

// current — a snapshot of the master for stage-then-commit transactions.
func (m *masterMachine) current() realtime.Peer {
	return m.peer
}

// restore — rollback of a staged transition on a broken commit.
func (m *masterMachine) restore(master realtime.Peer) {
	m.peer = master
}
