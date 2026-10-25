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

// runLifecycle применяет join/leave атомарно относительно тиков комнаты:
// мутация r.peers и вызов policy происходят в одном goroutine'е room-loop'а.
// Иначе между OnJoin (peer уже в policy state) и записью в r.peers успевал
// patch-тик, refreshPeerPings считал нового peer'а stale и выкидывал его
// из state (см. docs/plans/P13-lifecycle-peers-atomicity.md).
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
	// Занятый peerID — гонка double-join (два resume одного reconnect-
	// токена, P20): молчаливый overwrite сиротил бы проигравший peer-
	// объект вне r.peers (conn-утечка, его teardown встретит ErrPeerStale,
	// а кадры от него продолжат идти в room.Deliver). Проверка до capacity:
	// свой слот переполнением не считается. Resume-поток дожимает отказ
	// через Replace (newest-wins, Q10).
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
	// Identity-check (P20): изымаем только если под peerID лежит тот же
	// объект — stale teardown (старый conn, проснувшийся после
	// Replace/resume) не должен выкинуть нового peer'а.
	current, present := r.peers[peerID]
	if present && current != peer {
		r.mutex.Unlock()
		return domain.ErrPeerStale
	}
	// Декремент только по факту изъятия: повторный leave (оба caller'а
	// проскочили pre-check до сериализации на loop'е) не должен
	// дважды декрементить active_peers.
	delete(r.peers, peerID)
	r.mutex.Unlock()
	// При ошибке OnLeave state-запись peer'а само-вычищается следующим
	// prune-тиком refreshPeerPings (GetPeer → ErrPeerNotFound → stale).
	err := r.policy.OnLeave(peer)
	// Peer уже изъят из r.peers - leave считается и при ошибке policy.
	if present {
		r.metrics.PeerLeft()
		r.metrics.AddActivePeers(-1)
	}
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
