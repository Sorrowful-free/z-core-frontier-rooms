package realtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

// defaultPeerCloseDrainTimeout is the cap on the outbound drain at Stop
// for direct NewPeer construction (white-box tests); the production
// value is injected by PeerFactory via the field from
// TransportConfig.PeerCloseDrainTimeout. Duplicate partner of
// transport.DefaultPeerCloseDrainTimeout — keep them equal.
const defaultPeerCloseDrainTimeout = 1 * time.Second

type Peer struct {
	id           domain.PeerID
	nickName     string
	connection   transport.Connection
	outbound     chan events.PeerEvent
	logger       logging.Logger
	metrics      portmetrics.Metrics
	drainTimeout time.Duration
	writeDone    chan struct{}

	ctx      context.Context
	cancel   context.CancelFunc
	started  atomic.Bool
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewPeer(ctx context.Context, id domain.PeerID, nickName string, connection transport.Connection, outboundQueue int, logger logging.Logger) *Peer {
	ctx, cancel := context.WithCancel(ctx)
	if outboundQueue < 0 {
		outboundQueue = 0
	}
	return &Peer{
		id:         id,
		nickName:   nickName,
		connection: connection,
		outbound:   make(chan events.PeerEvent, outboundQueue),
		logger:     logger,
		// Defaults for direct construction (tests): Noop metrics and the
		// package drain default; production values are injected by
		// PeerFactory via the field (the signature stays stable for
		// white-box tests).
		metrics:      portmetrics.Noop(),
		drainTimeout: defaultPeerCloseDrainTimeout,
		writeDone:    make(chan struct{}),
		ctx:          ctx,
		cancel:       cancel,
	}
}

func (p *Peer) GetID() domain.PeerID {
	return p.id
}

func (p *Peer) GetNickName() string {
	return p.nickName
}

func (p *Peer) Ping() int64 {
	return p.connection.Ping()
}

func (p *Peer) IsAlive() bool {
	return p.connection.IsAlive()
}

func (p *Peer) Start(sink realtime.EventSink) error {
	p.started.Store(true)
	p.wg.Add(2)
	go processIncomingEvents(p, sink)
	go processOutgoingEvents(p)
	return nil
}

func (p *Peer) Stop() error {
	var err error
	p.stopOnce.Do(func() {
		p.cancel()
		// Drain phase (P30): frames committed to outbound before Stop get
		// a Send attempt before conn.Close — the write loop flushes the
		// queue and closes writeDone. Wait bounded (drainTimeout): on
		// deadline we go for a force close that interrupts even a blocked
		// Send. 0 — drain disabled (previous behavior). A separate
		// writeDone is mandatory: the whole wg cannot be waited before
		// conn.Close — the read loop hangs in Receive() until close
		// (deadlock). Stop without Start: the write loop is not running,
		// writeDone would never close — the drain wait is skipped via
		// the started flag.
		if p.drainTimeout > 0 && p.started.Load() {
			timer := time.NewTimer(p.drainTimeout)
			select {
			case <-p.writeDone:
			case <-timer.C:
			}
			timer.Stop()
		}
		err = p.connection.Close()
		p.wg.Wait()
		// outbound is not closed: tryDeliver producers are outside p.wg → send-on-closed race (ADR 0003).
	})
	return err
}

func (p *Peer) Deliver(peerEvent events.PeerEvent) error {
	return p.tryDeliver(peerEvent)
}

func processIncomingEvents(peer *Peer, sink realtime.EventSink) {
	defer peer.wg.Done()
	for {
		frame, err := peer.connection.Receive()
		if err != nil {
			return
		}
		if err := sink.Deliver(events.RoomEvent{
			PeerID: peer.id,
			Frame:  frame,
		}); err != nil {
			if errors.Is(err, domain.ErrQueueFull) {
				if frame.Delivery == domain.DeliveryUnreliable {
					peer.logger.Warn("room incoming queue full, dropping frame", "peerID", peer.id)
					continue
				}
				// A reliable frame is never silently dropped (P37): a peer
				// flooding the room mailbox is a zombie — the same
				// escalation as the fatal branch: close and let the read
				// loop exit into the transport teardown path.
				peer.logger.Warn("room incoming queue full, reliable frame undeliverable, closing connection", "peerID", peer.id)
				_ = peer.connection.Close()
				return
			}
			peer.logger.Error("error delivering room event", "error", err)
			// Fatal delivery error — speed up the connection cleanup.
			_ = peer.connection.Close()
			return
		}
	}
}

func processOutgoingEvents(peer *Peer) {
	defer peer.wg.Done()
	// writeDone is closed on exit from any branch — Stop waits on it as
	// the drain-phase boundary (the whole wg cannot be waited before
	// conn.Close).
	defer close(peer.writeDone)
	for {
		select {
		case <-peer.ctx.Done():
			// Drain phase (P30): ctx is cancelled — Deliver producers now
			// fail; scoop out the outbound remainder until empty and exit.
			// No re-sweep after observing empty is needed: a Deliver racing
			// with Stop is a fixed contract boundary (unspecified).
			for {
				select {
				case peerEvent := <-peer.outbound:
					if !sendFrame(peer, peerEvent) {
						return
					}
				default:
					return
				}
			}
		case peerEvent := <-peer.outbound:
			if !sendFrame(peer, peerEvent) {
				return
			}
		}
	}
}

// sendFrame writes one frame to the connection; false — the conn is
// dead or a write error occurred, the write loop must exit (the common
// path for the drain phase too).
func sendFrame(peer *Peer, peerEvent events.PeerEvent) bool {
	// Gauge sample of depth after dequeue (approximation, see the port).
	peer.metrics.PeerOutboundDepth(len(peer.outbound))
	if err := peer.connection.Send(peerEvent.Frame); err != nil {
		// Write error — the connection is dead (closed externally,
		// network failure): continuing is pointless — close and exit,
		// otherwise Error is logged for every queued frame. If the
		// connection is already marked dead — this is expected teardown
		// (evict/leave/Stop closed the conn before the frame), not a
		// failure: Debug instead of Error and no send-error counter.
		if peer.connection.IsAlive() {
			peer.metrics.PeerSendError()
			peer.logger.Error("error sending frame, closing connection", "error", err)
		} else {
			peer.logger.Debug("send on already closed connection, closing", "error", err)
		}
		_ = peer.connection.Close()
		return false
	}
	return true
}

func (p *Peer) tryDeliver(ev events.PeerEvent) error {
	if err := p.ctx.Err(); err != nil {
		return fmt.Errorf("peer stopped: %d: %w", p.id, err)
	}
	select {
	case <-p.ctx.Done():
		return fmt.Errorf("peer stopped: %d: %w", p.id, p.ctx.Err())
	case p.outbound <- ev:
		// Gauge sample of depth after enqueue (approximation, see the port).
		p.metrics.PeerOutboundDepth(len(p.outbound))
		// Race with Stop: ctx could be cancelled between the check above
		// and enqueue — the frame is queued, but the write loop may have
		// already seen it empty and exited (unspecified per the port
		// contract). An honest "peer stopped" instead of nil for a frame
		// that may not go out.
		if err := p.ctx.Err(); err != nil {
			return fmt.Errorf("peer stopped: %d: %w", p.id, err)
		}
		return nil
	default:
		if ev.Frame.Delivery == domain.DeliveryUnreliable {
			p.logger.Warn("peer outbound queue full, dropping frame", "peerID", p.id)
			p.metrics.PeerOutboundDropped()
			return domain.ErrQueueFull
		}
		// A reliable frame is never silently dropped (P37): a stalled or
		// flooded outbound makes the peer a zombie anyway — escalate to
		// teardown. No blocking send: the dead conn surfaces via the normal
		// path (write-loop sendFrame error / read error → DisconnectPeer →
		// grace).
		p.logger.Warn("peer outbound queue full, reliable frame undeliverable, closing connection", "peerID", p.id)
		_ = p.connection.Close()
		return domain.ErrQueueFull
	}
}
