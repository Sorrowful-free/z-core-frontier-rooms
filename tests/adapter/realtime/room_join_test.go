package realtime_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	portpolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

func TestJoinCallsSendFromOnJoinWithoutDeadlock(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("test")
	policy := &sendOnJoinPolicy{}
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(1), policy, 8, nil, 0, logger)
	policy.room = room

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	peer := testpeer.New(domain.PeerID(10), "")

	done := make(chan error, 1)
	go func() { done <- room.Join(peer) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Join: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Join blocked >2s (possible deadlock under handler mutex)")
	}

	if !room.HasPeer(peer.GetID()) {
		t.Fatal("peer not in room after Join")
	}
}

func TestJoinRollbackOnHandlerError(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("test")
	policy := &failOnJoinPolicy{err: domain.ErrJoinDenied}
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(1), policy, 8, nil, 0, logger)

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	peer := testpeer.New(domain.PeerID(11), "")
	err := room.Join(peer)
	if !errors.Is(err, domain.ErrJoinDenied) {
		t.Fatalf("Join err = %v", err)
	}
	if room.HasPeer(peer.GetID()) {
		t.Fatal("peer must be removed from room after failed OnJoin")
	}
}

// TestLifecyclePeerRegistryOrdering — регрессия P13: мутация r.peers и
// OnJoin/OnLeave атомарны на room-loop'е. До фикса peer попадал в policy
// state до записи в r.peers — patch-тик успевал прочитать state и выкинуть
// нового peer'а как stale (pruning stale peer). Spy-policy проверяет
// видимость изнутри колбэков: это та же точка наблюдения, что у тиков.
func TestLifecyclePeerRegistryOrdering(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("test")
	policy := &orderingProbePolicy{t: t}
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(1), policy, 8, nil, 0, logger)
	policy.room = room

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	peer := testpeer.New(domain.PeerID(20), "")
	if err := room.Join(peer); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if err := room.Leave(peer); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if policy.onJoinErr != nil {
		t.Fatalf("OnJoin observed: %v", policy.onJoinErr)
	}
	if policy.onLeaveErr != nil {
		t.Fatalf("OnLeave observed: %v", policy.onLeaveErr)
	}
}

// orderingProbePolicy — внутри OnJoin peer уже обязан быть в r.peers
// (иначе refreshPeerPings выкинул бы его как stale до следующего тика);
// внутри OnLeave — уже удалён.
type orderingProbePolicy struct {
	t    *testing.T
	room realtime.Room

	onJoinErr  error
	onLeaveErr error
}

func (p *orderingProbePolicy) OnStart(room realtime.Room) error {
	p.room = room
	return nil
}
func (p *orderingProbePolicy) OnStop(realtime.Room) error { return nil }
func (p *orderingProbePolicy) OnMessage(events.RoomEvent) error {
	return nil
}

func (p *orderingProbePolicy) OnJoin(peer realtime.Peer) error {
	_, p.onJoinErr = p.room.GetPeer(peer.GetID())
	return nil
}

func (p *orderingProbePolicy) OnLeave(peer realtime.Peer) error {
	_, err := p.room.GetPeer(peer.GetID())
	if !errors.Is(err, domain.ErrPeerNotFound) {
		p.onLeaveErr = err
	}
	return nil
}

func (p *orderingProbePolicy) TickIntervals() (time.Duration, time.Duration) { return 0, 0 }
func (p *orderingProbePolicy) OnTickFullState() error                        { return nil }
func (p *orderingProbePolicy) OnTickPatchState() error                       { return nil }

type sendOnJoinPolicy struct {
	room realtime.Room
}

func (p *sendOnJoinPolicy) OnStart(room realtime.Room) error {
	p.room = room
	return nil
}
func (p *sendOnJoinPolicy) OnStop(realtime.Room) error  { return nil }
func (p *sendOnJoinPolicy) OnLeave(realtime.Peer) error { return nil }
func (p *sendOnJoinPolicy) OnMessage(events.RoomEvent) error {
	return nil
}

func (p *sendOnJoinPolicy) OnJoin(peer realtime.Peer) error {
	return p.room.Send(events.PeerEvent{
		ExcludePeerID: peer.GetID(),
		Frame:         domain.Frame{OpCode: 0x01},
	})
}

func (p *sendOnJoinPolicy) TickIntervals() (time.Duration, time.Duration) { return 0, 0 }
func (p *sendOnJoinPolicy) OnTickFullState() error                        { return nil }
func (p *sendOnJoinPolicy) OnTickPatchState() error                       { return nil }

type failOnJoinPolicy struct {
	err error
}

func (p *failOnJoinPolicy) OnStart(realtime.Room) error { return nil }
func (p *failOnJoinPolicy) OnStop(realtime.Room) error  { return nil }
func (p *failOnJoinPolicy) OnLeave(realtime.Peer) error { return nil }
func (p *failOnJoinPolicy) OnMessage(events.RoomEvent) error {
	return nil
}
func (p *failOnJoinPolicy) OnJoin(realtime.Peer) error { return p.err }

func (p *failOnJoinPolicy) TickIntervals() (time.Duration, time.Duration) { return 0, 0 }
func (p *failOnJoinPolicy) OnTickFullState() error                        { return nil }
func (p *failOnJoinPolicy) OnTickPatchState() error                       { return nil }

var _ portpolicy.RoomPolicy = (*sendOnJoinPolicy)(nil)
