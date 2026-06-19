package registry_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

func TestRegistry_DeleteRoom_ConcurrentDeliver_NoPanic(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t)
	const roomID = domain.RoomID(50)

	room, err := reg.CreateRoom(context.Background(), roomID, 8, nil)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	ev := events.RoomEvent{PeerID: domain.PeerID(1), Frame: domain.Frame{OpCode: 0x01}}
	stop := make(chan struct{})
	var wg sync.WaitGroup
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

	time.Sleep(10 * time.Millisecond)

	if err := reg.DeleteRoom(context.Background(), roomID); err != nil {
		t.Fatalf("DeleteRoom: %v", err)
	}
	close(stop)
	wg.Wait()

	select {
	case <-room.Context().Done():
	default:
		t.Fatal("room context must be cancelled after DeleteRoom")
	}
}
