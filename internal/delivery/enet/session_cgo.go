//go:build enet && cgo

package enet

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/enet"
	libenet "github.com/codecat/go-enet"
)

type peerSession struct {
	id       uint16
	peer     libenet.Peer
	incoming chan domain.Frame
	conn     transport.Connection
	admitted bool
	roomID   domain.RoomID
	peerID   domain.PeerID
}

func newPeerSession(peer libenet.Peer, connectionFactory enet.EnetConnectionFactory, queueCap int) *peerSession {
	if queueCap <= 0 {
		queueCap = 256
	}
	incoming := make(chan domain.Frame, queueCap)
	connection := connectionFactory.CreateConnection(peer, incoming)
	return &peerSession{
		id:       peer.GetIncomingPeerId(),
		peer:     peer,
		incoming: incoming,
		conn:     connection,
	}
}

func (s *peerSession) deliver(frame domain.Frame) bool {
	select {
	case s.incoming <- frame:
		return true
	default:
		return false
	}
}

func (s *peerSession) clearPeerData() {
	if s.peer != nil {
		s.peer.SetData(nil)
	}
}

func (s *peerSession) close() error {
	return s.conn.Close()
}
