package realtime

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
)

type Room struct {
	id               domain.RoomID
	policy           policy.RoomPolicy
	mutex            sync.RWMutex
	peers            map[domain.PeerID]realtime.Peer
	capacity         int
	attributes       domain.RoomAttributes
	incoming         chan events.RoomEvent
	lifecycle        chan lifecycleRequest
	logger           logging.Logger
	metrics          portmetrics.Metrics
	fullStateTicker  *time.Ticker
	patchStateTicker *time.Ticker

	ctx       context.Context
	cancel    context.CancelFunc
	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup
}

func NewRoom(ctx context.Context, id domain.RoomID, policy policy.RoomPolicy, capacity int, attributes domain.RoomAttributes, incomingQueue int, logger logging.Logger) *Room {
	ctx, cancel := context.WithCancel(ctx)
	if incomingQueue < 0 {
		incomingQueue = 0
	}
	return &Room{
		id:         id,
		policy:     policy,
		mutex:      sync.RWMutex{},
		peers:      make(map[domain.PeerID]realtime.Peer),
		capacity:   capacity,
		attributes: domain.CloneRoomAttributes(attributes),
		incoming:   make(chan events.RoomEvent, incomingQueue),
		lifecycle:  make(chan lifecycleRequest),
		logger:     logger,
		// Прямое конструирование (тесты) - телеметрия-заглушка; продовый
		// экземпляр метрик вливает RoomFactory полем (сигнатура стабильна).
		metrics: portmetrics.Noop(),
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (r *Room) Context() context.Context {
	return r.ctx
}

func (r *Room) GetID() domain.RoomID {
	return r.id
}

func (r *Room) GetCapacity() int8 {
	if r.capacity > domain.MaxRoomCapacity {
		return int8(domain.MaxRoomCapacity)
	}
	if r.capacity < math.MinInt8 {
		return math.MinInt8
	}
	return int8(r.capacity)
}

func (r *Room) GetAttributes() domain.RoomAttributes {
	return domain.CloneRoomAttributes(r.attributes)
}

func (r *Room) GetPeers() []realtime.Peer {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	peers := make([]realtime.Peer, 0, len(r.peers))
	for _, p := range r.peers {
		peers = append(peers, p)
	}
	return peers
}

func (r *Room) Start() error {
	var err error
	r.startOnce.Do(func() {
		if err = r.policy.OnStart(r); err != nil {
			return
		}
		fullInterval, patchInterval := r.policy.TickIntervals()

		r.mutex.Lock()
		if fullInterval > 0 {
			r.fullStateTicker = time.NewTicker(fullInterval)
		}
		if patchInterval > 0 {
			r.patchStateTicker = time.NewTicker(patchInterval)
		}
		r.mutex.Unlock()

		r.wg.Add(1)
		go processRoomEvents(r)
	})
	return err
}

func (r *Room) Stop() error {
	var err error
	r.stopOnce.Do(func() {
		r.cancel()
		r.mutex.Lock()
		if r.fullStateTicker != nil {
			r.fullStateTicker.Stop()
		}
		if r.patchStateTicker != nil {
			r.patchStateTicker.Stop()
		}
		r.mutex.Unlock()
		r.wg.Wait()
		err = r.policy.OnStop(r)
	})
	return err
}

func (r *Room) Join(peer realtime.Peer) error {
	peerID := peer.GetID()

	r.mutex.RLock()
	_, exists := r.peers[peerID]
	full := len(r.peers) >= r.capacity
	r.mutex.RUnlock()
	// Занятый peerID пропускаем до lifecycle: там уже-существующий слот
	// отклоняется ErrPeerAlreadyInRoom (гонка double-resume → Replace),
	// а не ErrRoomFull — свой слот не должен считаться переполнением.
	if full && !exists {
		return fmt.Errorf("%w: %d", domain.ErrRoomFull, r.id)
	}

	// Авторитетная capacity-проверка и вставка в r.peers — внутри
	// runLifecycle на room-loop'е, атомарно с OnJoin относительно тиков.
	if err := r.dispatchLifecycle(lifecycleRequest{
		op:   lifecycleJoin,
		peer: peer,
		done: make(chan error, 1),
	}); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrJoinDenied, err)
	}
	return nil
}

func (r *Room) Leave(peer realtime.Peer) error {
	peerID := peer.GetID()

	r.mutex.RLock()
	if _, ok := r.peers[peerID]; !ok {
		r.mutex.RUnlock()
		return fmt.Errorf("%w: %d", domain.ErrPeerNotFound, peerID)
	}
	r.mutex.RUnlock()

	if err := r.dispatchLifecycle(lifecycleRequest{
		op:   lifecycleLeave,
		peer: peer,
		done: make(chan error, 1),
	}); err != nil {
		// Sentinel-отказы (ErrPeerNotFound — дедуп P23, ErrPeerStale —
		// вытеснение P20) — ожидаемые гонки teardown, до policy не
		// доходят: их логируют caller'ы со своим контекстом, здесь Error
		// был бы ложным «policy on leave failed».
		if !errors.Is(err, domain.ErrPeerNotFound) && !errors.Is(err, domain.ErrPeerStale) {
			r.logger.Error("room leave: policy on leave failed", "error", err, "roomID", r.id, "peerID", peerID)
		}
		return err
	}
	return nil
}

func (r *Room) Replace(peer realtime.Peer) error {
	peerID := peer.GetID()

	r.mutex.RLock()
	old, ok := r.peers[peerID]
	r.mutex.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %d", domain.ErrPeerNotFound, peerID)
	}

	if err := r.dispatchLifecycle(lifecycleRequest{
		op:   lifecycleLeave,
		peer: old,
		done: make(chan error, 1),
	}); err != nil {
		// Old уже изъят из r.peers до OnLeave (leave всегда вытесняет,
		// ADR-0004 D4): teardown обязателен, иначе conn и goroutine'ы
		// живут вне комнаты - утечка и ghost-input (P20). Безобидно и
		// при ErrPeerStale (old вытеснен конкурентным Replace - его уже
		// остановил вытеснитель) и при умирающей комнате: stopOnce
		// делает повторный Stop no-op.
		stopErr := old.Stop()
		return fmt.Errorf("%w: %w", domain.ErrReplaceFailed, errors.Join(err, stopErr))
	}

	if err := r.dispatchLifecycle(lifecycleRequest{
		op:   lifecycleJoin,
		peer: peer,
		done: make(chan error, 1),
	}); err != nil {
		rollbackErr := r.dispatchLifecycle(lifecycleRequest{
			op:   lifecycleJoin,
			peer: old,
			done: make(chan error, 1),
		})
		if rollbackErr != nil {
			// Старый peer вытеснен необратимо (rollback не вернул его в
			// r.peers): teardown обязателен, иначе conn и goroutine'ы живут
			// вне комнаты - утечка и ghost-input (triple-resume v4, P20).
			stopErr := old.Stop()
			return fmt.Errorf("%w: %w", domain.ErrReplaceFailed, errors.Join(err, rollbackErr, stopErr))
		}
		return fmt.Errorf("%w: %w", domain.ErrReplaceFailed, err)
	}

	if err := old.Stop(); err != nil {
		return err
	}
	return nil
}

func (r *Room) HasPeer(peerID domain.PeerID) bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	_, ok := r.peers[peerID]
	return ok
}

func (r *Room) GetPeer(peerID domain.PeerID) (realtime.Peer, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	peer, ok := r.peers[peerID]
	if !ok {
		return nil, fmt.Errorf("%w: %d", domain.ErrPeerNotFound, peerID)
	}
	return peer, nil
}

func (r *Room) Send(peerEvent events.PeerEvent) error {
	peers, err := r.snapshotPeersForSend(peerEvent.PeerID, peerEvent.ExcludePeerID)
	if err != nil {
		return err
	}
	for _, peer := range peers {
		if err := peer.Deliver(peerEvent); err != nil {
			if errors.Is(err, domain.ErrQueueFull) {
				r.logger.Warn("peer outbound queue full during send", "roomID", r.id, "peerID", peer.GetID())
				continue
			}
			return err
		}
	}
	return nil
}

func (r *Room) snapshotPeersForSend(target, exclude domain.PeerID) ([]realtime.Peer, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	if target.IsValid() {
		peer, ok := r.peers[target]
		if !ok {
			return nil, fmt.Errorf("%w: %d", domain.ErrPeerNotFound, target)
		}
		return []realtime.Peer{peer}, nil
	}

	peers := make([]realtime.Peer, 0, len(r.peers))
	for id, peer := range r.peers {
		if exclude.IsValid() && id == exclude {
			continue
		}
		peers = append(peers, peer)
	}
	return peers, nil
}

func (r *Room) Deliver(roomEvent events.RoomEvent) error {
	return r.tryDeliver(roomEvent)
}

func processRoomEvents(room *Room) {
	defer room.wg.Done()

	var fullTick <-chan time.Time
	if room.fullStateTicker != nil {
		fullTick = room.fullStateTicker.C
	}
	var patchTick <-chan time.Time
	if room.patchStateTicker != nil {
		patchTick = room.patchStateTicker.C
	}

	for {
		select {
		case <-room.ctx.Done():
			return
		case roomEvent, ok := <-room.incoming:
			if !ok {
				return
			}
			// Gauge-сэмпл глубины после dequeue (приближение, см. порт).
			room.metrics.RoomIncomingDepth(len(room.incoming))
			if err := room.policy.OnMessage(roomEvent); err != nil {
				room.logger.Error("error processing room event", "error", err)
				room.notifyInRoomError(roomEvent.PeerID, err)
			}
		case req, ok := <-room.lifecycle:
			if !ok {
				return
			}
			room.runLifecycle(req)
		case <-fullTick:
			started := time.Now()
			if err := room.policy.OnTickFullState(); err != nil {
				room.logger.Error("tick full state", "error", err)
			}
			room.metrics.RoomTickObserved(tickKindFull, time.Since(started))
		case <-patchTick:
			started := time.Now()
			if err := room.policy.OnTickPatchState(); err != nil {
				room.logger.Error("tick patch state", "error", err)
			}
			room.metrics.RoomTickObserved(tickKindPatch, time.Since(started))
		}
	}
}

// Значения label `kind` для RoomTickObserved (bounded-множество в адаптере).
const (
	tickKindFull  = "full"
	tickKindPatch = "patch"
)

func (r *Room) tryDeliver(ev events.RoomEvent) error {
	if err := r.ctx.Err(); err != nil {
		return fmt.Errorf("room stopped: %d: %w", r.id, err)
	}
	select {
	case <-r.ctx.Done():
		return fmt.Errorf("room stopped: %d: %w", r.id, r.ctx.Err())
	case r.incoming <- ev:
		// Gauge-сэмпл глубины после enqueue (приближение, см. порт).
		r.metrics.RoomIncomingDepth(len(r.incoming))
		return nil
	default:
		r.logger.Warn("room incoming queue full, dropping frame", "roomID", r.id, "peerID", ev.PeerID)
		r.metrics.RoomIncomingDropped()
		return domain.ErrQueueFull
	}
}
