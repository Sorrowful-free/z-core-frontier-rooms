package identity_test

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	adapteridentity "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/identity"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	portidentity "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/identity"
)

var _ portidentity.Allocator = (*adapteridentity.Counter)(nil)

func TestAllocateRoomID_FirstIsValid(t *testing.T) {
	t.Parallel()

	counter := adapteridentity.NewCounter()
	id, err := counter.AllocateRoomID(context.Background())
	if err != nil {
		t.Fatalf("AllocateRoomID: %v", err)
	}
	if id != domain.RoomID(1) {
		t.Fatalf("id = %v, want 1", id)
	}
	if !id.IsValid() {
		t.Fatalf("id = %v, want valid", id)
	}
}

func TestAllocatePeerID_FirstIsValid(t *testing.T) {
	t.Parallel()

	counter := adapteridentity.NewCounter()
	id, err := counter.AllocatePeerID(context.Background())
	if err != nil {
		t.Fatalf("AllocatePeerID: %v", err)
	}
	if id != domain.PeerID(1) {
		t.Fatalf("id = %v, want 1", id)
	}
	if !id.IsValid() {
		t.Fatalf("id = %v, want valid", id)
	}
}

func TestAllocateRoomAndPeer_IndependentSequences(t *testing.T) {
	t.Parallel()

	counter := adapteridentity.NewCounter()
	ctx := context.Background()

	room1, err := counter.AllocateRoomID(ctx)
	if err != nil {
		t.Fatalf("AllocateRoomID 1: %v", err)
	}
	peer1, err := counter.AllocatePeerID(ctx)
	if err != nil {
		t.Fatalf("AllocatePeerID 1: %v", err)
	}
	room2, err := counter.AllocateRoomID(ctx)
	if err != nil {
		t.Fatalf("AllocateRoomID 2: %v", err)
	}
	peer2, err := counter.AllocatePeerID(ctx)
	if err != nil {
		t.Fatalf("AllocatePeerID 2: %v", err)
	}

	if room1 != domain.RoomID(1) || room2 != domain.RoomID(2) {
		t.Fatalf("room ids = %v, %v, want 1, 2", room1, room2)
	}
	if peer1 != domain.PeerID(1) || peer2 != domain.PeerID(2) {
		t.Fatalf("peer ids = %v, %v, want 1, 2", peer1, peer2)
	}
}

func TestAllocate_CancelledContext(t *testing.T) {
	t.Parallel()

	counter := adapteridentity.NewCounter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := counter.AllocateRoomID(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AllocateRoomID err = %v, want context.Canceled", err)
	}

	_, err = counter.AllocatePeerID(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AllocatePeerID err = %v, want context.Canceled", err)
	}
}

func TestAllocateRoomID_WrapSkipsZero(t *testing.T) {
	t.Parallel()

	counter := adapteridentity.NewCounterAt(math.MaxUint32, 0)

	id, err := counter.AllocateRoomID(context.Background())
	if err != nil {
		t.Fatalf("AllocateRoomID: %v", err)
	}
	if id != domain.RoomID(1) {
		t.Fatalf("id after wrap = %v, want 1", id)
	}
}

func TestAllocatePeerID_WrapSkipsZero(t *testing.T) {
	t.Parallel()

	counter := adapteridentity.NewCounterAt(0, math.MaxUint32)

	id, err := counter.AllocatePeerID(context.Background())
	if err != nil {
		t.Fatalf("AllocatePeerID: %v", err)
	}
	if id != domain.PeerID(1) {
		t.Fatalf("id after wrap = %v, want 1", id)
	}
}

func TestAllocateRoomID_ConcurrentUnique(t *testing.T) {
	t.Parallel()

	counter := adapteridentity.NewCounter()
	const n = 256

	seen := make(map[domain.RoomID]struct{}, n)
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(n)

	errCh := make(chan error, n)
	for range n {
		go func() {
			defer wg.Done()
			id, err := counter.AllocateRoomID(context.Background())
			if err != nil {
				errCh <- err
				return
			}
			mu.Lock()
			if _, dup := seen[id]; dup {
				mu.Unlock()
				errCh <- errors.New("duplicate room id")
				return
			}
			seen[id] = struct{}{}
			mu.Unlock()
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != n {
		t.Fatalf("unique ids = %d, want %d", len(seen), n)
	}
}
