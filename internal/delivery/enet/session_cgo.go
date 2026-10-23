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
	libenet "github.com/codecat/go-enet"
)

type peerSession struct {
	id          uint16
	host        *enetHost // для least-loaded счётчика (P18)
	incoming    chan domain.Frame
	conn        transport.Connection
	admitted    bool
	roomID      domain.RoomID
	peerID      domain.PeerID
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
	// GetIncomingPeerId - peer-операция, диспатч через очередь loop'а
	// (newPeerSession зовётся с run-горутины, не с loop-горутины).
	id, _ := transportenet.Call(loop, func() uint16 { return peer.GetIncomingPeerId() })
	// Живой peer на host'е: счётчик для AssignHost (least-loaded)
	// и gauge peers-per-host (P19b) - то же число, что видит балансировка.
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

// close - единственная точка снятия сессии: каждый путь удаления из sessions
// идёт через неё, поэтому счётчик живых peer'ов host'а декрементируется здесь
// (once - повторные close безопасны, как и conn.Close).
func (s *peerSession) close() error {
	s.closeOnce.Do(func() {
		peers := s.host.peers.Add(-1)
		s.metrics.EnetHostPeers(s.host.id, int(peers))
	})
	return s.conn.Close()
}
