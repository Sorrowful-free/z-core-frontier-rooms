package realtime_test

import (
	"context"
	"testing"

	adapterrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type joinBenchPeer struct {
	id domain.PeerID
}

func (p *joinBenchPeer) GetID() domain.PeerID    { return p.id }
func (p *joinBenchPeer) GetNickName() string     { return "join" }
func (p *joinBenchPeer) Ping() int64             { return 0 }
func (p *joinBenchPeer) Start() error           { return nil }
func (p *joinBenchPeer) Stop() error            { return nil }
func (p *joinBenchPeer) Deliver(events.PeerEvent) error { return nil }

var _ realtime.Peer = (*joinBenchPeer)(nil)

func BenchmarkRoom_Join(b *testing.B) {
	b.Run("first_peer", func(b *testing.B) {
		var sink error
		for b.Loop() {
			room := adapterrealtime.NewRoom(
				context.Background(),
				domain.RoomID(1),
				noopPolicy{},
				8,
				4,
				queueTestLogger{},
			)
			if err := room.Start(); err != nil {
				b.Fatal(err)
			}
			peer := &joinBenchPeer{id: domain.PeerID(1)}
			sink = room.Join(peer)
			_ = room.Stop()
		}
		_ = sink
	})
}
