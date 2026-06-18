package realtime

import (
	"context"
	"errors"
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
	connection transport.Connection
	room       realtime.Room
	outbound   chan events.PeerEvent
	logger     logging.Logger

	ctx      context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewPeer(ctx context.Context, id domain.PeerID, nickName string, connection transport.Connection, room realtime.Room, outboundQueue int, logger logging.Logger) *Peer {
	ctx, cancel := context.WithCancel(ctx)
	if outboundQueue < 0 {
		outboundQueue = 0
	}
	return &Peer{
		id:         id,
		nickName:   nickName,
		connection: connection,
		room:       room,
		outbound:   make(chan events.PeerEvent, outboundQueue),
		logger:     logger,
		ctx:        ctx,
		cancel:     cancel,
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

func (p *Peer) Start() error {
	p.wg.Add(2)
	go processIncomingEvents(p)
	go processOutgoingEvents(p)
	return nil
}

func (p *Peer) Stop() error {
	var err error
	p.stopOnce.Do(func() {
		p.cancel()
		err = p.connection.Close()
		p.wg.Wait()
		close(p.outbound)
	})
	return err
}

func (p *Peer) Deliver(peerEvent events.PeerEvent) error {
	return p.tryDeliver(peerEvent)
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
			if errors.Is(err, domain.ErrQueueFull) {
				peer.logger.Warn("room incoming queue full, dropping frame", "peerID", peer.id)
				continue
			}
			peer.logger.Error("error delivering room event", "error", err)
			return
		}
	}
}

func processOutgoingEvents(peer *Peer) {
	defer peer.wg.Done()
	for {
		select {
		case <-peer.ctx.Done():
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

func (p *Peer) tryDeliver(ev events.PeerEvent) error {
	if err := p.ctx.Err(); err != nil {
		return fmt.Errorf("peer stopped: %d: %w", p.id, err)
	}
	select {
	case <-p.ctx.Done():
		return fmt.Errorf("peer stopped: %d: %w", p.id, p.ctx.Err())
	case p.outbound <- ev:
		return nil
	default:
		p.logger.Warn("peer outbound queue full, dropping frame", "peerID", p.id)
		return domain.ErrQueueFull
	}
}
