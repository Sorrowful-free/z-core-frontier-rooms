//go:build enet && cgo

package enet

import (
	"sync"
	"time"

	transportenet "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport/enet"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
	libenet "github.com/codecat/go-enet"
)

type peerSession struct {
	id       uint16
	host     *enetHost // for the least-loaded counter (P18)
	incoming chan domain.Frame
	conn     transport.Connection
	admitted bool
	// roomSession — session handle of the admitted connection (P20):
	// teardown goes through DisconnectPeer with exactly this peer object
	// (identity check).
	roomSession room.PeerSession
	connectedAt time.Time
	closeOnce   sync.Once
	metrics     portmetrics.Metrics
}

func newPeerSession(host *enetHost, loop *transportenet.HostLoop, peer libenet.Peer, connectionFactory enet.EnetConnectionFactory, queueCap int, m portmetrics.Metrics) *peerSession {
	if queueCap < 0 {
		queueCap = 0
	}
	incoming := make(chan domain.Frame, queueCap)
	connection := connectionFactory.CreateConnection(peer, incoming)
	// GetIncomingPeerId - a peer operation, dispatched through the loop's
	// queue (newPeerSession is called from the run goroutine, not the loop
	// goroutine).
	id, _ := transportenet.Call(loop, func() uint16 { return peer.GetIncomingPeerId() })
	// A live peer on the host: counter for AssignHost (least-loaded) and
	// the peers-per-host gauge (P19b) - the same number the balancing sees.
	peers := host.peers.Add(1)
	metrics := portmetrics.OrNoop(m)
	metrics.EnetHostPeers(host.id, int(peers))
	return &peerSession{
		id:          id,
		host:        host,
		incoming:    incoming,
		conn:        connection,
		connectedAt: time.Now(),
		metrics:     metrics,
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

// close - the single teardown point of a session: every path removing it from
// sessions goes through here, so the host's live-peer counter is decremented
// here (once - repeated close is safe, as is conn.Close).
func (s *peerSession) close() error {
	s.closeOnce.Do(func() {
		peers := s.host.peers.Add(-1)
		s.metrics.EnetHostPeers(s.host.id, int(peers))
	})
	return s.conn.Close()
}
