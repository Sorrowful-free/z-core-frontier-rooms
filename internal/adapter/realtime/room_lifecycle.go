package realtime

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type lifecycleOp byte

const (
	lifecycleJoin lifecycleOp = iota
	lifecycleLeave
)

type lifecycleRequest struct {
	op   lifecycleOp
	peer realtime.Peer
	done chan error
}

// runLifecycle applies join/leave atomically relative to the room ticks:
// the r.peers mutation and the policy call happen in the same room-loop
// goroutine. Otherwise, between OnJoin (the peer is already in policy
// state) and the write to r.peers, a patch tick could slip in whose
// prune (now pruneStalePeers) would consider the new peer stale and
// drop it from state (see docs/plans/P13-lifecycle-peers-atomicity.md).
func (r *Room) runLifecycle(req lifecycleRequest) {
	var err error
	switch req.op {
	case lifecycleJoin:
		err = r.runJoinLocked(req.peer)
	case lifecycleLeave:
		err = r.runLeaveLocked(req.peer)
	default:
		err = fmt.Errorf("unknown lifecycle op: %d", req.op)
	}
	req.done <- err
}

func (r *Room) runJoinLocked(peer realtime.Peer) error {
	peerID := peer.GetID()
	r.mutex.Lock()
	// An occupied peerID is a double-join race (two resumes of the same
	// reconnect token, P20): a silent overwrite would orphan the losing
	// peer object outside r.peers (a conn leak, its teardown would hit
	// ErrPeerStale, while its frames keep flowing into room.Deliver).
	// The check runs before capacity: one's own slot does not count as
	// overflow. The resume flow pushes the rejection through via Replace
	// (newest-wins, Q10).
	if _, exists := r.peers[peerID]; exists {
		r.mutex.Unlock()
		return fmt.Errorf("%w: %d", domain.ErrPeerAlreadyInRoom, peerID)
	}
	if len(r.peers) >= r.capacity {
		r.mutex.Unlock()
		return fmt.Errorf("%w: %d", domain.ErrRoomFull, r.id)
	}
	r.peers[peerID] = peer
	r.mutex.Unlock()

	if err := r.policy.OnJoin(peer); err != nil {
		r.mutex.Lock()
		delete(r.peers, peerID)
		r.mutex.Unlock()
		return err
	}
	r.metrics.PeerJoined()
	r.metrics.AddActivePeers(1)
	return nil
}

func (r *Room) runLeaveLocked(peer realtime.Peer) error {
	peerID := peer.GetID()
	r.mutex.Lock()
	// Identity check (P20): remove only if the same object sits under
	// peerID — a stale teardown (an old conn waking up after
	// Replace/resume) must not evict the new peer.
	current, present := r.peers[peerID]
	if present && current != peer {
		r.mutex.Unlock()
		return fmt.Errorf("%w: %d", domain.ErrPeerStale, peerID)
	}
	// Double-teardown dedup (P23) is authoritative at the registry level:
	// the peer is already removed — a repeated leave (both callers slipped
	// past the Room.Leave pre-check before serialization on the loop) is
	// rejected with the same sentinel as the pre-check — "peer not in the
	// room at apply time" is unified across both levels. Rejection happens
	// before delete, without calling policy.OnLeave and without metrics:
	// a single broadcast must not depend on what a concrete policy returns
	// for a missing state record.
	if !present {
		r.mutex.Unlock()
		return fmt.Errorf("%w: %d", domain.ErrPeerNotFound, peerID)
	}
	delete(r.peers, peerID)
	r.mutex.Unlock()
	// On an OnLeave error the peer's state record self-cleans on the next
	// prune pass of the patch tick (pruneStalePeers: GetPeer → ErrPeerNotFound → stale).
	err := r.policy.OnLeave(peer)
	// The peer is already removed from r.peers — leave counts even on a
	// policy error; metrics follow the fact of removal (we only reach
	// here when present).
	r.metrics.PeerLeft()
	r.metrics.AddActivePeers(-1)
	return err
}

func (r *Room) dispatchLifecycle(req lifecycleRequest) error {
	select {
	case <-r.ctx.Done():
		return fmt.Errorf("room stopped: %d: %w", r.id, r.ctx.Err())
	case r.lifecycle <- req:
	}
	select {
	case err := <-req.done:
		return err
	case <-r.ctx.Done():
		return fmt.Errorf("room stopped: %d: %w", r.id, r.ctx.Err())
	}
}
