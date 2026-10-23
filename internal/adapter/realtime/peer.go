package realtime

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
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
	metrics    portmetrics.Metrics

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
		// Умолчание Noop (прямое конструирование в тестах); продовый
		// экземпляр метрик вливает PeerFactory полем.
		metrics: portmetrics.Noop(),
		ctx:     ctx,
		cancel:  cancel,
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
		// outbound не закрываем: tryDeliver-продюсеры вне p.wg → send-on-closed race (ADR 0003).
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
			// Фатальная ошибка доставки — ускоряем cleanup соединения.
			_ = peer.connection.Close()
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
		case peerEvent := <-peer.outbound:
			// Gauge-сэмпл глубины после dequeue (приближение, см. порт).
			peer.metrics.PeerOutboundDepth(len(peer.outbound))
			if err := peer.connection.Send(peerEvent.Frame); err != nil {
				// Ошибка записи — соединение мертво (закрыт извне, сетевой
				// сбой): продолжать бессмысленно — закрываем и выходим,
				// иначе Error логируется на каждый кадр очереди.
				// Если соединение уже помечено неживым — это ожидаемый
				// teardown (evict/leave/Stop закрыл conn раньше кадра),
				// не сбой: Debug вместо Error и без счётчика send-ошибок.
				if peer.connection.IsAlive() {
					peer.metrics.PeerSendError()
					peer.logger.Error("error sending frame, closing connection", "error", err)
				} else {
					peer.logger.Debug("send on already closed connection, closing", "error", err)
				}
				_ = peer.connection.Close()
				return
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
		// Gauge-сэмпл глубины после enqueue (приближение, см. порт).
		p.metrics.PeerOutboundDepth(len(p.outbound))
		return nil
	default:
		p.logger.Warn("peer outbound queue full, dropping frame", "peerID", p.id)
		p.metrics.PeerOutboundDropped()
		return domain.ErrQueueFull
	}
}
