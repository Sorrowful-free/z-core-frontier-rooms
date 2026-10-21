package realtime_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	portpolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
)

type noopPolicy struct{}

func (noopPolicy) OnStart(realtime.Room) error                   { return nil }
func (noopPolicy) OnStop(realtime.Room) error                    { return nil }
func (noopPolicy) OnJoin(realtime.Peer) error                    { return nil }
func (noopPolicy) OnLeave(realtime.Peer) error                   { return nil }
func (noopPolicy) OnMessage(events.RoomEvent) error              { return nil }
func (noopPolicy) TickIntervals() (time.Duration, time.Duration) { return 0, 0 }
func (noopPolicy) OnTickFullState() error                        { return nil }
func (noopPolicy) OnTickPatchState() error                       { return nil }

var _ portpolicy.RoomPolicy = noopPolicy{}

func startTestRoom(t *testing.T, incomingQueue int) *adapterrealtime.Room {
	t.Helper()

	room := adapterrealtime.NewRoom(
		context.Background(),
		domain.RoomID(99),
		noopPolicy{},
		32,
		nil,
		incomingQueue,
		queueTestLogger{},
	)
	if err := room.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return room
}

func TestRoom_Stop_ConcurrentDeliver_NoPanic(t *testing.T) {
	t.Parallel()

	room := startTestRoom(t, 128)
	ev := events.RoomEvent{PeerID: domain.PeerID(1), Frame: domain.Frame{OpCode: 0x01}}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	const producers = 8

	for range producers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = room.Deliver(ev)
				}
			}
		}()
	}

	time.Sleep(10 * time.Millisecond)

	if err := room.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	close(stop)
	wg.Wait()

	assertRoomStopped(t, room)
}

func TestRoom_Stop_ConcurrentJoin_NoPanic(t *testing.T) {
	t.Parallel()

	room := startTestRoom(t, 64)

	var wg sync.WaitGroup
	const joiners = 16
	for i := range joiners {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			peer := &stubPeer{id: domain.PeerID(id + 100)}
			_ = room.Join(peer)
		}(i)
	}

	go func() { _ = room.Stop() }()

	wg.Wait()
	_ = room.Stop()

	assertRoomStopped(t, room)
}

func TestRoom_Stop_ConcurrentLeave_NoPanic(t *testing.T) {
	t.Parallel()

	room := startTestRoom(t, 64)

	peers := make([]*stubPeer, 4)
	for i := range peers {
		peers[i] = &stubPeer{id: domain.PeerID(i + 200)}
		if err := room.Join(peers[i]); err != nil {
			t.Fatalf("Join peer %d: %v", i, err)
		}
	}

	var wg sync.WaitGroup
	for _, peer := range peers {
		wg.Add(1)
		go func(p *stubPeer) {
			defer wg.Done()
			_ = room.Leave(p)
		}(peer)
	}

	if err := room.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	wg.Wait()

	assertRoomStopped(t, room)
}

func TestRoom_Stop_LifecycleErrorWrapsContextCanceled(t *testing.T) {
	t.Parallel()

	room := startTestRoom(t, 64)

	peer := &stubPeer{id: domain.PeerID(300)}
	if err := room.Join(peer); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if err := room.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if err := room.Join(&stubPeer{id: domain.PeerID(301)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Join after Stop err = %v, want errors.Is context.Canceled", err)
	}
	if err := room.Leave(peer); !errors.Is(err, context.Canceled) {
		t.Fatalf("Leave after Stop err = %v, want errors.Is context.Canceled", err)
	}
}

func assertRoomStopped(t *testing.T, room *adapterrealtime.Room) {
	t.Helper()

	select {
	case <-room.Context().Done():
	default:
		t.Fatal("room context must be cancelled after Stop")
	}
}
