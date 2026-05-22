package realtime

import (
	"fmt"
	"sync"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

type Peer struct {
	id         domain.PeerID
	nickName   string
	ping       int64
	connection transport.Connection
	room       realtime.Room
	outbound   chan events.PeerEvent
	logger     logging.Logger

	done     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewPeer(id domain.PeerID, connection transport.Connection, room realtime.Room, logger logging.Logger) *Peer {
	return &Peer{
		id:         id,
		connection: connection,
		room:       room,
		outbound:   make(chan events.PeerEvent),
		logger:     logger,
		done:       make(chan struct{}),
	}
}

func (p *Peer) GetID() domain.PeerID {
	return p.id
}

func (p *Peer) GetNickName() string {
	return p.nickName
}

func (p *Peer) GetPing() int64 {
	return p.ping
}

func (p *Peer) Start() error {
	p.wg.Add(2)
	go processIncomingEvents(p)
	go processOutgoingEvents(p)
	return nil
}

func (p *Peer) Stop() error {
	var err error
	p.stopOnce.Do(func() {
		close(p.done)
		err = p.connection.Close()
		p.wg.Wait()
		close(p.outbound)
	})
	return err
}

func (p *Peer) Deliver(peerEvent events.PeerEvent) error {
	if !p.tryDeliver(peerEvent) {
		return fmt.Errorf("peer stopped: %s", p.id)
	}
	return nil
}

func processIncomingEvents(peer *Peer) {
	defer peer.wg.Done()
	for {
		frame, err := peer.connection.Receive()
		if err != nil {
			return
		}
		if err := peer.room.Deliver(events.RoomEvent{
			PeerID: peer.id,
			Frame:  frame,
		}); err != nil {
			peer.logger.Error("error delivering room event", "error", err)
			return
		}
	}
}

func processOutgoingEvents(peer *Peer) {
	defer peer.wg.Done()
	for {
		select {
		case <-peer.done:
			return
		case peerEvent, ok := <-peer.outbound:
			if !ok {
				return
			}
			if err := peer.connection.Send(peerEvent.Frame); err != nil {
				peer.logger.Error("error sending frame", "error", err)
			}
		}
	}
}

func (p *Peer) tryDeliver(ev events.PeerEvent) bool {
	select {
	case <-p.done:
		return false
	case p.outbound <- ev:
		return true
	}
}
