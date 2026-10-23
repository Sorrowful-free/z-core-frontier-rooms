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
	if len(r.peers) >= r.capacity {
		r.mutex.Unlock()
		return fmt.Errorf("%w: %d", domain.ErrRoomFull, r.id)
	}
	// Метрика членства привязана к реальной мутации map: join считается
	// только если peerID реально добавлен (не перезаписан), иначе
	// active_peers уедет в дрейф при повторном join того же peer.
	_, existed := r.peers[peerID]
	r.peers[peerID] = peer
	r.mutex.Unlock()

	if err := r.policy.OnJoin(peer); err != nil {
		r.mutex.Lock()
		delete(r.peers, peerID)
		r.mutex.Unlock()
		return err
	}
	if !existed {
		r.metrics.PeerJoined()
		r.metrics.AddActivePeers(1)
	}
	return nil
}

func (r *Room) runLeaveLocked(peer realtime.Peer) error {
	peerID := peer.GetID()
	r.mutex.Lock()
	// Декремент только по факту изъятия: повторный leave (оба caller'а
	// проскочили pre-check до сериализации на loop'е) не должен
	// дважды декрементить active_peers.
	_, removed := r.peers[peerID]
	delete(r.peers, peerID)
	r.mutex.Unlock()
	// При ошибке OnLeave state-запись peer'а само-вычищается следующим
	// prune-тиком refreshPeerPings (GetPeer → ErrPeerNotFound → stale).
	err := r.policy.OnLeave(peer)
	// Peer уже изъят из r.peers - leave считается и при ошибке policy.
	if removed {
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
