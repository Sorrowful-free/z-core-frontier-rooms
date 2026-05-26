package registry_test

import (
	"context"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func TestShutdownStopsAllRooms(t *testing.T) {
	t.Parallel()

	reg := newTestRegistry(t)

	const roomID = domain.RoomID(42)
	room, err := reg.CreateRoom(context.Background(), roomID, 8)
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if room.Context().Err() != nil {
		t.Fatal("room context must be active before shutdown")
	}

	if err := reg.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if _, err := reg.GetRoom(context.Background(), roomID); err == nil {
		t.Fatal("GetRoom after Shutdown: want error")
	}
	select {
	case <-room.Context().Done():
	default:
		t.Fatal("room context must be cancelled after shutdown")
	}
}
