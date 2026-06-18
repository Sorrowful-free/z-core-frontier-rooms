package realtime_test

import (
	"context"
	"testing"

	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

func BenchmarkRoom_Deliver(b *testing.B) {
	room := adapterrealtime.NewRoom(
		context.Background(),
		domain.RoomID(10),
		noopPolicy{},
		8,
		64,
		queueTestLogger{},
	)
	if err := room.Start(); err != nil {
		b.Fatalf("Start: %v", err)
	}
	b.Cleanup(func() { _ = room.Stop() })

	ev := events.RoomEvent{
		PeerID: domain.PeerID(1),
		Frame:  domain.Frame{OpCode: 0x01, Payload: []byte{0xBB}},
	}

	var sink error
	b.ResetTimer()
	for b.Loop() {
		sink = room.Deliver(ev)
	}
	_ = sink
}
