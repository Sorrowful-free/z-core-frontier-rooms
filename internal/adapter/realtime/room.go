package realtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
)

type Room struct {
	id       domain.RoomID
	policy   policy.RoomPolicy
	mutex    sync.RWMutex
	peers    map[domain.PeerID]realtime.Peer
	capacity int
	incoming         chan events.RoomEvent
	lifecycle        chan lifecycleRequest
	logger           logging.Logger
	fullStateTicker  *time.Ticker
	patchStateTicker *time.Ticker

	ctx      context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewRoom(ctx context.Context, id domain.RoomID, policy policy.RoomPolicy, capacity int, logger logging.Logger) *Room {
	ctx, cancel := context.WithCancel(ctx)
	return &Room{
		id:       id,
		policy:   policy,
		mutex:    sync.RWMutex{},
		peers:    make(map[domain.PeerID]realtime.Peer),
		capacity: capacity,
		incoming:  make(chan events.RoomEvent),
		lifecycle: make(chan lifecycleRequest),
		logger:    logger,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (r *Room) Context() context.Context {
	return r.ctx
}

func (r *Room) GetID() domain.RoomID {
	return r.id
}

func (r *Room) GetCapacity() int8 {
	if r.capacity > 127 {
		return 127
	}
	if r.capacity < -128 {
		return -128
	}
	return int8(r.capacity)
}

func (r *Room) GetPeers() []realtime.Peer {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	peers := make([]realtime.Peer, 0, len(r.peers))
	for _, p := range r.peers {
		peers = append(peers, p)
	}
	return peers
}

func (r *Room) Start() error {
	if err := r.policy.OnStart(r); err != nil {
		return err
	}
	fullInterval, patchInterval := r.policy.TickIntervals()
	if fullInterval > 0 {
		r.fullStateTicker = time.NewTicker(fullInterval)
	}
	if patchInterval > 0 {
		r.patchStateTicker = time.NewTicker(patchInterval)
	}

	r.wg.Add(1)
	go processRoomEvents(r)
	return nil
}

func (r *Room) Stop() error {
	var err error
	r.stopOnce.Do(func() {
		r.cancel()
		if r.fullStateTicker != nil {
			r.fullStateTicker.Stop()
		}
		if r.patchStateTicker != nil {
			r.patchStateTicker.Stop()
		}
		r.wg.Wait()
		close(r.incoming)
		close(r.lifecycle)
		err = r.policy.OnStop(r)
	})
	return err
}

func (r *Room) Join(peer realtime.Peer) error {
	peerID := peer.GetID()

	r.mutex.Lock()
	if len(r.peers) >= r.capacity {
		r.mutex.Unlock()
		return fmt.Errorf("%w: %d", domain.ErrRoomFull, r.id)
	}
	r.peers[peerID] = peer
	r.mutex.Unlock()

	err := r.dispatchLifecycle(lifecycleRequest{
		op:   lifecycleJoin,
		peer: peer,
		done: make(chan error, 1),
	})
	if err != nil {
		r.mutex.Lock()
		delete(r.peers, peerID)
		r.mutex.Unlock()
		return fmt.Errorf("%w: %w", domain.ErrJoinDenied, err)
	}
	return nil
}

func (r *Room) Leave(peer realtime.Peer) error {
	peerID := peer.GetID()

	r.mutex.Lock()
	if _, ok := r.peers[peerID]; !ok {
		r.mutex.Unlock()
		return fmt.Errorf("peer not found: %d", peerID)
	}
	r.mutex.Unlock()

	if err := r.dispatchLifecycle(lifecycleRequest{
		op:   lifecycleLeave,
		peer: peer,
		done: make(chan error, 1),
	}); err != nil {
		return err
	}

	r.mutex.Lock()
	delete(r.peers, peerID)
	r.mutex.Unlock()
	return nil
}

func (r *Room) Replace(peer realtime.Peer) error {
	peerID := peer.GetID()

	r.mutex.Lock()
	old, ok := r.peers[peerID]
	if !ok {
		r.mutex.Unlock()
		return fmt.Errorf("%w: %d", domain.ErrPeerNotFound, peerID)
	}
	r.mutex.Unlock()

	if err := r.dispatchLifecycle(lifecycleRequest{
		op:      lifecycleReplace,
		peer:    peer,
		oldPeer: old,
		done:    make(chan error, 1),
	}); err != nil {
		return err
	}

	r.mutex.Lock()
	r.peers[peerID] = peer
	r.mutex.Unlock()

	if err := old.Stop(); err != nil {
		return err
	}
	return nil
}

func (r *Room) HasPeer(peerID domain.PeerID) bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	_, ok := r.peers[peerID]
	return ok
}

func (r *Room) GetPeer(peerID domain.PeerID) (realtime.Peer, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	peer, ok := r.peers[peerID]
	if !ok {
		return nil, fmt.Errorf("peer not found: %d", peerID)
	}
	return peer, nil
}

func (r *Room) Send(peerEvent events.PeerEvent) error {
	peers, err := r.snapshotPeersForSend(peerEvent.PeerID, peerEvent.ExcludePeerID)
	if err != nil {
		return err
	}
	for _, peer := range peers {
		if err := peer.Deliver(peerEvent); err != nil {
			return err
		}
	}
	return nil
}

func (r *Room) snapshotPeersForSend(target, exclude domain.PeerID) ([]realtime.Peer, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	if target.IsValid() {
		peer, ok := r.peers[target]
		if !ok {
			return nil, fmt.Errorf("peer not found: %d", target)
		}
		return []realtime.Peer{peer}, nil
	}

	peers := make([]realtime.Peer, 0, len(r.peers))
	for id, peer := range r.peers {
		if exclude.IsValid() && id == exclude {
			continue
		}
		peers = append(peers, peer)
	}
	return peers, nil
}

func (r *Room) Deliver(roomEvent events.RoomEvent) error {
	if !r.tryDeliver(roomEvent) {
		return fmt.Errorf("room stopped: %d", r.id)
	}
	return nil
}

func processRoomEvents(room *Room) {
	defer room.wg.Done()

	var fullTick <-chan time.Time
	if room.fullStateTicker != nil {
		fullTick = room.fullStateTicker.C
	}
	var patchTick <-chan time.Time
	if room.patchStateTicker != nil {
		patchTick = room.patchStateTicker.C
	}

	for {
		select {
		case <-room.ctx.Done():
			return
		case roomEvent, ok := <-room.incoming:
			if !ok {
				return
			}
			if err := room.policy.OnMessage(roomEvent); err != nil {
				room.logger.Error("error processing room event", "error", err)
			}
		case req, ok := <-room.lifecycle:
			if !ok {
				return
			}
			room.runLifecycle(req)
		case <-fullTick:
			if err := room.policy.OnTickFullState(); err != nil {
				room.logger.Error("tick full state", "error", err)
			}
		case <-patchTick:
			if err := room.policy.OnTickPatchState(); err != nil {
				room.logger.Error("tick patch state", "error", err)
			}
		}
	}
}

func (r *Room) tryDeliver(ev events.RoomEvent) bool {
	select {
	case <-r.ctx.Done():
		return false
	case r.incoming <- ev:
		return true
	}
}
