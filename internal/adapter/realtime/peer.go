package realtime

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

type Peer struct {
	id         domain.PeerID
	nickName   string
	ping       int64
	connection transport.Connection
	incoming   chan events.PeerEvent
	logger     logging.Logger

	done chan struct{}
}

func NewPeer(id domain.PeerID, connection transport.Connection, logger logging.Logger) *Peer {
	return &Peer{
		id:         id,
		connection: connection,
		incoming:   make(chan events.PeerEvent),
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
	go processIncomingEvents(p)
	go processOutgoingEvents(p)
	return nil
}

func (p *Peer) Stop() error {
	close(p.done)
	close(p.incoming)
	if err := p.connection.Close(); err != nil {
		return err
	}
	return nil
}

func (p *Peer) GetIncoming() chan<- events.PeerEvent {
	return p.incoming
}

func processIncomingEvents(peer *Peer) {
	for {
		select {
		case <-peer.done:
			return
		case frame, ok := <-peer.connection.GetIncoming():
			if !ok {
				return
			}
			peer.incoming <- events.PeerEvent{
				PeerID: peer.id,
				Frame:  frame,
			}
		}
	}
}

func processOutgoingEvents(peer *Peer) {
	for {
		select {
		case <-peer.done:
			return
		case peerEvent := <-peer.incoming:
			frame := peerEvent.Frame
			if err := peer.connection.Send(frame); err != nil {
				peer.logger.Error("error sending frame", "error", err)
			}
		}
	}
}
