package registry_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/logging/stdlib"
	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	statepolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/policy/state"
	adapterregistry "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/registry"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	portpolicy "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
)

// gatingRoomFactory holds CreateRoom at a gate until the test releases it —
// so both concurrent calls are guaranteed to overlap before the registry write.
type gatingRoomFactory struct {
	inner   realtime.RoomFactory
	arrived chan struct{}
	gate    chan struct{}

	mu      sync.Mutex
	created []realtime.Room
}

func (f *gatingRoomFactory) CreateRoom(ctx context.Context, id domain.RoomID, capacity int, attributes domain.RoomAttributes) (realtime.Room, error) {
	room, err := f.inner.CreateRoom(ctx, id, capacity, attributes)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.created = append(f.created, room)
	f.mu.Unlock()
	f.arrived <- struct{}{}
	<-f.gate
	return room, nil
}

func TestCreateRoom_ConcurrentSameID_SingleWinner(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("test")
	inner := adapterrealtime.NewRoomFactory(logger, statepolicy.NewStateRoomPolicyFactory(logger, statepolicy.StatePolicyConfig{AllowMasterMigration: true}, nil), 0, nil)
	factory := &gatingRoomFactory{
		inner:   inner,
		arrived: make(chan struct{}, 2),
		gate:    make(chan struct{}),
	}
	reg := adapterregistry.NewRoomRegistry(context.Background(), factory, logger, nil)
	t.Cleanup(func() { _ = reg.Shutdown(context.Background()) })

	const id = domain.RoomID(77)

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := reg.CreateRoom(context.Background(), id, 4, nil)
			results <- err
		}()
	}

	for i := 0; i < 2; i++ {
		select {
		case <-factory.arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("room factory arrivals timed out")
		}
	}
	close(factory.gate)
	wg.Wait()
	close(results)

	created, already := 0, 0
	for err := range results {
		switch {
		case err == nil:
			created++
		case errors.Is(err, domain.ErrRoomAlreadyExists):
			already++
		default:
			t.Fatalf("unexpected CreateRoom err = %v", err)
		}
	}
	if created != 1 || already != 1 {
		t.Fatalf("created = %d, alreadyExists = %d, want 1/1", created, already)
	}

	rooms, err := reg.GetList(context.Background())
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	if len(rooms) != 1 {
		t.Fatalf("rooms = %d, want 1", len(rooms))
	}

	// the losing room is stopped: its ctx is cancelled
	stopped := 0
	for _, room := range factory.created {
		select {
		case <-room.Context().Done():
			stopped++
		default:
		}
	}
	if stopped != 1 {
		t.Fatalf("stopped created rooms = %d, want 1", stopped)
	}
}

// stallStopPolicy blocks in OnStop until release — simulates a slow room.Stop.
type stallStopPolicy struct {
	entered chan struct{}
	release chan struct{}
}

func (p *stallStopPolicy) OnStart(realtime.Room) error {
	return nil
}
func (p *stallStopPolicy) OnStop(realtime.Room) error {
	close(p.entered)
	<-p.release
	return nil
}
func (p *stallStopPolicy) OnJoin(realtime.Peer) error  { return nil }
func (p *stallStopPolicy) OnLeave(realtime.Peer) error { return nil }
func (p *stallStopPolicy) OnMessage(events.RoomEvent) error {
	return nil
}
func (p *stallStopPolicy) TickIntervals() (time.Duration, time.Duration) {
	return 0, 0
}
func (p *stallStopPolicy) OnTickFullState() error  { return nil }
func (p *stallStopPolicy) OnTickPatchState() error { return nil }

var _ portpolicy.RoomPolicy = (*stallStopPolicy)(nil)

type fixedPolicyFactory struct {
	policy portpolicy.RoomPolicy
}

func (f fixedPolicyFactory) CreateRoomPolicy() portpolicy.RoomPolicy {
	return f.policy
}

func TestDeleteRoom_SlowStop_GetListNotBlocked(t *testing.T) {
	t.Parallel()

	logger := stdlib.New("test")
	policy := &stallStopPolicy{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	roomFactory := adapterrealtime.NewRoomFactory(logger, fixedPolicyFactory{policy: policy}, 0, nil)
	reg := adapterregistry.NewRoomRegistry(context.Background(), roomFactory, logger, nil)
	t.Cleanup(func() { _ = reg.Shutdown(context.Background()) })

	const id = domain.RoomID(88)
	if _, err := reg.CreateRoom(context.Background(), id, 4, nil); err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	deleteDone := make(chan error, 1)
	go func() {
		deleteDone <- reg.DeleteRoom(context.Background(), id)
	}()

	select {
	case <-policy.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("room Stop not entered")
	}

	// GetList must not wait for the in-flight Stop: the room is already removed from the registry.
	type listResult struct {
		count int
		err   error
	}
	listDone := make(chan listResult, 1)
	go func() {
		rooms, err := reg.GetList(context.Background())
		listDone <- listResult{count: len(rooms), err: err}
	}()

	select {
	case res := <-listDone:
		if res.err != nil {
			t.Fatalf("GetList: %v", res.err)
		}
		if res.count != 0 {
			t.Fatalf("rooms during stop = %d, want 0", res.count)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetList blocked by in-flight room Stop")
	}

	close(policy.release)
	select {
	case err := <-deleteDone:
		if err != nil {
			t.Fatalf("DeleteRoom: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DeleteRoom did not return after release")
	}
}
