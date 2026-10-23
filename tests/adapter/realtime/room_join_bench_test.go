package realtime_test

import (
	"context"
	"testing"

	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

func BenchmarkRoom_Join(b *testing.B) {
	b.Run("first_peer", func(b *testing.B) {
		var sink error
		for b.Loop() {
			room := adapterrealtime.NewRoom(
				context.Background(),
				domain.RoomID(1),
				noopPolicy{},
				8,
				nil,
				4,
				queueTestLogger{},
			)
			if err := room.Start(); err != nil {
				b.Fatal(err)
			}
			peer := testpeer.New(domain.PeerID(1), "join", testpeer.WithoutCapture())
			sink = room.Join(peer)
			_ = room.Stop()
		}
		_ = sink
	})
}
