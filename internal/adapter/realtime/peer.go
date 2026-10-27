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

// defaultPeerCloseDrainTimeout — потолок drain'а outbound при Stop для
// прямого конструирования NewPeer (white-box тесты); продовое значение
// вливает PeerFactory полем из TransportConfig.PeerCloseDrainTimeout.
// Дубль-партнёр transport.DefaultPeerCloseDrainTimeout — держать равными.
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
		// Умолчания при прямом конструировании (тесты): Noop-метрики и
		// пакетный drain-дефолт; продовые значения вливает PeerFactory
		// полем (сигнатура стабильна для white-box тестов).
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
		// Drain-фаза (P30): кадры, закоммиченные в outbound до Stop,
		// получают попытку Send до conn.Close — write-loop сливает
		// очередь и замыкает writeDone. Ждать bounded (drainTimeout):
		// по дедлайну идём на force close, прерывающий даже
		// заблокированный Send. 0 — drain выключен (прежнее поведение).
		// Отдельный writeDone обязателен: ждать весь wg до conn.Close
		// нельзя — read-loop висит в Receive() до close (deadlock).
		// Stop без Start: write-loop не запущен, writeDone не замкнётся
		// никогда — drain-ожидание пропускаем по started-флагу.
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
		// outbound не закрываем: tryDeliver-продюсеры вне p.wg → send-on-closed race (ADR 0003).
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
	// writeDone замыкается при выходе из любой ветки — Stop ждёт его как
	// границу drain-фазы (весь wg ждать нельзя до conn.Close).
	defer close(peer.writeDone)
	for {
		select {
		case <-peer.ctx.Done():
			// Drain-фаза (P30): ctx отменён — продюсеры Deliver теперь
			// фейлятся; выгребаем остаток outbound до пустоты и выходим.
			// Sweep-повтор после наблюдения пустоты не нужен: Deliver,
			// гонящийся со Stop, — зафиксированная граница контракта
			// (unspecified).
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

// sendFrame пишет один кадр на соединение; false — conn мёртв/ошибка
// записи, write-loop должен выйти (общий путь и для drain-фазы).
func sendFrame(peer *Peer, peerEvent events.PeerEvent) bool {
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
		// Gauge-сэмпл глубины после enqueue (приближение, см. порт).
		p.metrics.PeerOutboundDepth(len(p.outbound))
		// Гонка со Stop: ctx мог отмениться между проверкой выше и
		// enqueue — кадр в очереди, но write-loop уже мог увидеть её
		// пустой и выйти (unspecified по контракту порта). Честный
		// «peer stopped» вместо nil за кадр, который может не уйти.
		if err := p.ctx.Err(); err != nil {
			return fmt.Errorf("peer stopped: %d: %w", p.id, err)
		}
		return nil
	default:
		p.logger.Warn("peer outbound queue full, dropping frame", "peerID", p.id)
		p.metrics.PeerOutboundDropped()
		return domain.ErrQueueFull
	}
}
