package realtime_test

import (
	"context"
	"testing"

	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	benchfixtures "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/bench"
)

type sendBenchPeer struct {
	id domain.PeerID
}

func (p *sendBenchPeer) GetID() domain.PeerID    { return p.id }
func (p *sendBenchPeer) GetNickName() string     { return "bench" }
func (p *sendBenchPeer) Ping() int64             { return 0 }
func (p *sendBenchPeer) Start() error           { return nil }
func (p *sendBenchPeer) Stop() error            { return nil }
func (p *sendBenchPeer) Deliver(events.PeerEvent) error { return nil }

var _ realtime.Peer = (*sendBenchPeer)(nil)

func setupBroadcastRoom(b *testing.B, peerCount int) *adapterrealtime.Room {
	b.Helper()

	room := adapterrealtime.NewRoom(
		context.Background(),
		domain.RoomID(1),
		noopPolicy{},
		peerCount,
		nil,
		64,
		queueTestLogger{},
	)
	if err := room.Start(); err != nil {
		b.Fatalf("Start: %v", err)
	}
	b.Cleanup(func() { _ = room.Stop() })

	for i := 1; i <= peerCount; i++ {
		peer := &sendBenchPeer{id: domain.PeerID(i)}
		if err := room.Join(peer); err != nil {
			b.Fatalf("Join peer %d: %v", i, err)
		}
	}
	return room
}

func BenchmarkRoom_Send_Broadcast(b *testing.B) {
	ev := events.PeerEvent{Frame: domain.Frame{OpCode: 0x01, Payload: []byte{0xAA}}}

	for _, peerCount := range []int{8, 32, 64} {
		b.Run(benchfixtures.PeerCountLabel(peerCount), func(b *testing.B) {
			room := setupBroadcastRoom(b, peerCount)

			var sink error
			b.ResetTimer()
			for b.Loop() {
				sink = room.Send(ev)
			}
			_ = sink
		})
	}
}
