package realtime

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type Room struct {
	id       domain.RoomID
	handler  realtime.RoomHandler
	peers    map[domain.PeerID]realtime.Peer
	incoming chan events.RoomEvent
	logger   logging.Logger

	done chan struct{}
}

func NewRoom(id domain.RoomID, handler realtime.RoomHandler, logger logging.Logger) *Room {
	return &Room{
		id:       id,
		handler:  handler,
		peers:    make(map[domain.PeerID]realtime.Peer),
		incoming: make(chan events.RoomEvent),
		logger:   logger,
		done:     make(chan struct{}),
	}
}

func (r *Room) GetID() domain.RoomID {
	return r.id
}

func (r *Room) Start() error {
	go processRoomEvents(r)
	if err := r.handler.OnStart(r); err != nil {
		r.Stop()
		return err
	}
	return nil
}

func (r *Room) Stop() error {
	close(r.done)
	close(r.incoming)
	return r.handler.OnStop(r)
}

func (r *Room) Join(peer realtime.Peer) error {
	if err := r.handler.OnJoin(peer); err != nil {
		return err
	}
	r.peers[peer.GetID()] = peer
	return nil
}

func (r *Room) Leave(peer realtime.Peer) error {
	if err := r.handler.OnLeave(peer); err != nil {
		return err
	}
	delete(r.peers, peer.GetID())
	return nil
}

func (r *Room) Send(peerEvent events.PeerEvent) error {
	peer, ok := r.peers[peerEvent.PeerID]
	if !ok {
		return fmt.Errorf("peer not found: %s", peerEvent.PeerID)
	}
	peer.GetIncoming() <- peerEvent
	return nil
}

func (r *Room) GetIncoming() chan<- events.RoomEvent {
	return r.incoming
}

func processRoomEvents(room *Room) {
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
