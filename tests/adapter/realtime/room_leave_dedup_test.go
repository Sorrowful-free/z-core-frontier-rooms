package realtime_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	metricsadapter "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/metrics"
	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	portpolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
	"github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/prom"
)

// P23: дедуп двойного leave авторитетен на уровне реестра, а не policy.
// Room-loop припарковывается внутри заблокированного OnMessage — оба
// caller'а Leave гарантированно проходят pre-check (peer ещё в r.peers)
// и сериализуются на lifecycle-канале. Проигравший обязан получить
// ErrPeerNotFound (тот же sentinel, что у pre-check Room.Leave), policy
// видит ровно один OnLeave, peer_leaves/active_peers декрементятся один раз.
func TestRoom_Leave_ConcurrentDoubleLeave_RegistryDedup(t *testing.T) {
	t.Parallel()

	m, err := metricsadapter.New(metricsadapter.MetricsConfig{})
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}
	policy := &gatedLeavePolicy{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	// Комната с продовым wiring метрик: RoomFactory вливает metrics полем,
	// policy подставляется spy-стаб через статическую фабрику.
	factory := adapterrealtime.NewRoomFactory(stdlib.New("test"), staticPolicyFactory{policy: policy}, 8, m)
	room, err := factory.CreateRoom(context.Background(), domain.RoomID(7), 8, nil, domain.RoomPolicyKindState)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	peer := testpeer.New(domain.PeerID(1), "host")
	if err := room.Join(peer); err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Паркуем loop: пока OnMessage заблокирован, delete в runLeaveLocked
	// выполнить некому — оба Leave пройдут pre-check и встанут на отправку
	// в lifecycle-канал.
	if err := room.Deliver(events.RoomEvent{PeerID: peer.GetID(), Frame: domain.Frame{OpCode: 0x01}}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	<-policy.entered

	results := make(chan error, 2)
	go func() { results <- room.Leave(peer) }()
	go func() { results <- room.Leave(peer) }()

	close(policy.release)

	var succeeded, notFound int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, domain.ErrPeerNotFound):
			notFound++
		default:
			t.Fatalf("Leave err = %v, want nil or ErrPeerNotFound", err)
		}
	}
	if succeeded != 1 || notFound != 1 {
		t.Fatalf("double leave: succeeded=%d notFound=%d, want 1/1", succeeded, notFound)
	}
	if got := policy.onLeaves.Load(); got != 1 {
		t.Fatalf("OnLeave calls = %d, want 1 (дедуп до policy)", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_peer_leaves_total", nil); got != 1 {
		t.Fatalf("peer_leaves = %v, want 1", got)
	}
	if got := prom.Value(t, m.Gatherer(), "rooms_active_peers", nil); got != 0 {
		t.Fatalf("active_peers = %v, want 0", got)
	}
}

// gatedLeavePolicy — spy-стаб policy: OnMessage блокирует room-loop до
// close(release), давая тесту детерминированное окно «peer ещё в реестре,
// но оба leave уже сериализованы». OnLeave считает вызовы — дедуп
// проверяется по тому, что повторный leave до policy не доходит.
type gatedLeavePolicy struct {
	roomCtx  context.Context
	entered  chan struct{} // закрывается при входе loop'а в OnMessage
	release  chan struct{} // close отпускает заблокированный OnMessage
	once     sync.Once
	onLeaves atomic.Int32
}

func (p *gatedLeavePolicy) OnStart(room realtime.Room) error {
	p.roomCtx = room.Context()
	return nil
}
func (p *gatedLeavePolicy) OnStop(realtime.Room) error { return nil }
func (p *gatedLeavePolicy) OnJoin(realtime.Peer) error { return nil }
func (p *gatedLeavePolicy) OnLeave(realtime.Peer) error {
	p.onLeaves.Add(1)
	return nil
}
func (p *gatedLeavePolicy) OnMessage(events.RoomEvent) error {
	p.once.Do(func() { close(p.entered) })
	// roomCtx — аварийный выход: при падении теста до close(release)
	// Stop не должен висеть на wg.Wait() за припаркованным loop'ом.
	select {
	case <-p.release:
	case <-p.roomCtx.Done():
	}
	return nil
}
func (p *gatedLeavePolicy) TickIntervals() (time.Duration, time.Duration) { return 0, 0 }
func (p *gatedLeavePolicy) OnTickFullState() error                        { return nil }
func (p *gatedLeavePolicy) OnTickPatchState() error                       { return nil }

var _ portpolicy.RoomPolicy = (*gatedLeavePolicy)(nil)

// staticPolicyFactory — RoomPolicyFactory, отдающая готовый policy-стаб
// (продовые фабрики конструируют policy сами; в тесте нужен счётчик
// вызовов, поэтому инстанс пробрасывается снаружи).
type staticPolicyFactory struct {
	policy portpolicy.RoomPolicy
}

func (f staticPolicyFactory) CreateRoomPolicy() portpolicy.RoomPolicy { return f.policy }

var _ portpolicy.RoomPolicyFactory = staticPolicyFactory{}
