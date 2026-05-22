package realtime

import (
	"fmt"
	"sync"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type Room struct {
	id       domain.RoomID
	handler  realtime.RoomHandler
	mutex    sync.RWMutex
	peers    map[domain.PeerID]realtime.Peer
	incoming chan events.RoomEvent
	logger   logging.Logger

	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewRoom(id domain.RoomID, handler realtime.RoomHandler, logger logging.Logger) *Room {
	return &Room{
		id:       id,
		handler:  handler,
		mutex:    sync.RWMutex{},
		peers:    make(map[domain.PeerID]realtime.Peer),
		incoming: make(chan events.RoomEvent),
		logger:   logger,
		done:     make(chan struct{}),
	}
}

func (r *Room) GetID() domain.RoomID {
	return r.id
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
	r.wg.Add(1)
	go processRoomEvents(r)
	if err := r.handler.OnStart(r); err != nil {
		r.Stop()
		return err
	}
	return nil
}

func (r *Room) Stop() error {
	var err error
	r.stopOnce.Do(func() {
		close(r.done)
		r.wg.Wait()
		close(r.incoming)
		err = r.handler.OnStop(r)
	})
	return err
}

func (r *Room) Join(peer realtime.Peer) error {
	peerID := peer.GetID()

	r.mutex.Lock()
	r.peers[peerID] = peer
	r.mutex.Unlock()

	if err := r.handler.OnJoin(peer); err != nil {
		r.mutex.Lock()
		delete(r.peers, peerID)
		r.mutex.Unlock()
		return err
	}
	return nil
}

func (r *Room) Leave(peer realtime.Peer) error {
	peerID := peer.GetID()

	r.mutex.Lock()
	if _, ok := r.peers[peerID]; !ok {
		r.mutex.Unlock()
		return fmt.Errorf("peer not found: %s", peerID)
	}
	r.mutex.Unlock()

	if err := r.handler.OnLeave(peer); err != nil {
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
		return fmt.Errorf("peer not found: %s", peerID)
	}
	r.mutex.Unlock()

	if err := r.handler.OnLeave(old); err != nil {
		return err
	}
	if err := r.handler.OnJoin(peer); err != nil {
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
		return nil, fmt.Errorf("peer not found: %s", peerID)
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
			return nil, fmt.Errorf("peer not found: %s", target)
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
		return fmt.Errorf("room stopped: %s", r.id)
	}
	return nil
}

func processRoomEvents(room *Room) {
	defer room.wg.Done()
	for {
		select {
		case <-room.done:
			return
		case roomEvent, ok := <-room.incoming:
			if !ok {
				return
			}
			if err := room.handler.OnMessage(roomEvent); err != nil {
				room.logger.Error("error processing room event", "error", err)
			}
		}
	}
}

func (r *Room) tryDeliver(ev events.RoomEvent) bool {
	select {
	case <-r.done:
		return false
	case r.incoming <- ev:
		return true
	}
}
