package realtime_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	portpolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
)

func TestRoom_Leave_PolicyError_StillRemovesPeer(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("test")
	policy := &failOnLeavePolicy{err: errors.New("on leave failed")}
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(1), policy, 8, nil, 0, logger)

	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	peer := &stubPeer{id: domain.PeerID(11)}
	if err := room.Join(peer); err != nil {
		t.Fatalf("Join: %v", err)
	}

	if err := room.Leave(peer); err == nil {
		t.Fatal("Leave: want error from failed OnLeave")
	}
	if room.HasPeer(peer.id) {
		t.Fatal("peer must be removed from room even when OnLeave fails")
	}

	if err := room.Leave(peer); !errors.Is(err, domain.ErrPeerNotFound) {
		t.Fatalf("second Leave err = %v, want ErrPeerNotFound", err)
	}
}

func TestRoom_Join_ConcurrentJoins_ExactlyOneFillsLastSlot(t *testing.T) {
	t.Parallel()

	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(3), noopPolicy{}, 1, nil, 8, queueTestLogger{})
	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	type joinResult struct {
		id  domain.PeerID
		err error
	}
	results := make(chan joinResult, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func(id domain.PeerID) {
			defer wg.Done()
			results <- joinResult{id: id, err: room.Join(&stubPeer{id: id})}
		}(domain.PeerID(i + 1))
	}
	wg.Wait()
	close(results)

	var winners []domain.PeerID
	for res := range results {
		switch {
		case res.err == nil:
			winners = append(winners, res.id)
		case errors.Is(res.err, domain.ErrRoomFull):
		default:
			t.Fatalf("Join peer %d: unexpected err %v", res.id, res.err)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("successful joins = %d, want exactly 1", len(winners))
	}
	if !room.HasPeer(winners[0]) {
		t.Fatalf("winner peer %d not in room", winners[0])
	}
}

func TestRoom_Start_Twice_RunsStartupOnce(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("test")
	policy := &countingStartPolicy{}
	room := adapterrealtime.NewRoom(context.Background(), domain.RoomID(2), policy, 8, nil, 0, logger)

	if err := room.Start(); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := room.Start(); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	t.Cleanup(func() { _ = room.Stop() })

	if got := policy.onStartCalls.Load(); got != 1 {
		t.Fatalf("OnStart calls = %d, want 1", got)
	}
	if got := policy.tickIntervalCalls.Load(); got != 1 {
		t.Fatalf("TickIntervals calls = %d, want 1", got)
	}
}

type failOnLeavePolicy struct {
	err error
}

func (p *failOnLeavePolicy) OnStart(realtime.Room) error      { return nil }
func (p *failOnLeavePolicy) OnStop(realtime.Room) error       { return nil }
func (p *failOnLeavePolicy) OnJoin(realtime.Peer) error       { return nil }
func (p *failOnLeavePolicy) OnLeave(realtime.Peer) error      { return p.err }
func (p *failOnLeavePolicy) OnMessage(events.RoomEvent) error { return nil }
func (p *failOnLeavePolicy) TickIntervals() (time.Duration, time.Duration) {
	return 0, 0
}
func (p *failOnLeavePolicy) OnTickFullState() error  { return nil }
func (p *failOnLeavePolicy) OnTickPatchState() error { return nil }

type countingStartPolicy struct {
	onStartCalls      atomic.Int32
	tickIntervalCalls atomic.Int32
}

func (p *countingStartPolicy) OnStart(realtime.Room) error {
	p.onStartCalls.Add(1)
	return nil
}
func (p *countingStartPolicy) OnStop(realtime.Room) error       { return nil }
func (p *countingStartPolicy) OnJoin(realtime.Peer) error       { return nil }
func (p *countingStartPolicy) OnLeave(realtime.Peer) error      { return nil }
func (p *countingStartPolicy) OnMessage(events.RoomEvent) error { return nil }
func (p *countingStartPolicy) TickIntervals() (time.Duration, time.Duration) {
	p.tickIntervalCalls.Add(1)
	return 0, 0
}
func (p *countingStartPolicy) OnTickFullState() error  { return nil }
func (p *countingStartPolicy) OnTickPatchState() error { return nil }

var _ portpolicy.RoomPolicy = (*failOnLeavePolicy)(nil)
var _ portpolicy.RoomPolicy = (*countingStartPolicy)(nil)
