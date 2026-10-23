//go:build enet && cgo

package enet

import (
	"time"

	transportenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/enet"
	libenet "github.com/codecat/go-enet"
)

type peerSession struct {
	id          uint16
	incoming    chan domain.Frame
	conn        transport.Connection
	admitted    bool
	roomID      domain.RoomID
	peerID      domain.PeerID
	connectedAt time.Time
}

func newPeerSession(loop *transportenet.HostLoop, peer libenet.Peer, connectionFactory enet.EnetConnectionFactory, queueCap int) *peerSession {
	if queueCap < 0 {
		queueCap = 0
	}
	incoming := make(chan domain.Frame, queueCap)
	connection := connectionFactory.CreateConnection(peer, incoming)
	// GetIncomingPeerId - peer-операция, диспатч через очередь loop'а
	// (newPeerSession зовётся с run-горутины, не с loop-горутины).
	id, _ := transportenet.Call(loop, func() uint16 { return peer.GetIncomingPeerId() })
	return &peerSession{
		id:          id,
		incoming:    incoming,
		conn:        connection,
		connectedAt: time.Now(),
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

func (s *peerSession) close() error {
	return s.conn.Close()
}
