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

// P23: dedup of a double leave is authoritative at the registry level, not the policy.
// The room loop is parked inside a blocked OnMessage — both Leave callers
// are guaranteed to pass the pre-check (peer still in r.peers) and serialize
// on the lifecycle channel. The loser must get ErrPeerNotFound (the same
// sentinel as the Room.Leave pre-check), the policy sees exactly one
// OnLeave, peer_leaves/active_peers are decremented once.
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
	// Room with production metrics wiring: RoomFactory injects metrics as a
	// field, the policy is substituted with a spy stub via a static factory.
	factory := adapterrealtime.NewRoomFactory(stdlib.New("test"), staticPolicyFactory{policy: policy}, 8, m)
	room, err := factory.CreateRoom(context.Background(), domain.RoomID(7), 8, nil)
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

	// Park the loop: while OnMessage is blocked, nothing can run the delete
	// in runLeaveLocked — both Leaves pass the pre-check and queue up to send
	// on the lifecycle channel.
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

// gatedLeavePolicy — a spy-stub policy: OnMessage blocks the room loop until
// close(release), giving the test a deterministic window "peer still in the
// registry, but both leaves already serialized". OnLeave counts calls —
// dedup is verified by the second leave never reaching the policy.
type gatedLeavePolicy struct {
	roomCtx  context.Context
	entered  chan struct{} // closed when the loop enters OnMessage
	release  chan struct{} // close releases the blocked OnMessage
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
	// roomCtx — emergency exit: if the test fails before close(release),
	// Stop must not hang on wg.Wait() behind the parked loop.
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

// staticPolicyFactory — a RoomPolicyFactory that hands out a ready-made
// policy stub (production factories construct the policy themselves; the
// test needs a call counter, so the instance is passed in from outside).
type staticPolicyFactory struct {
	policy portpolicy.RoomPolicy
}

func (f staticPolicyFactory) CreateRoomPolicy() portpolicy.RoomPolicy { return f.policy }

var _ portpolicy.RoomPolicyFactory = staticPolicyFactory{}
