package state_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

func BenchmarkOnTickPatchState(b *testing.B) {
	b.Run("peers=8", func(b *testing.B) {
		h := newBenchHarness(b, domain.RoomID(1), 16)
		peers := make([]*benchCapturePeer, 8)
		for i := range peers {
			peers[i] = newBenchCapturePeer(domain.PeerID(i+1), "peer")
		}
		mustJoinBench(b, h.room, peers...)

		if err := h.policy.OnTickPatchState(); err != nil {
			b.Fatal(err)
		}
		for _, p := range peers {
			p.clear()
		}

		var sink error
		b.ResetTimer()
		for b.Loop() {
			peers[0].ping++
			sink = h.policy.OnTickPatchState()
		}
		_ = sink
	})
}

func BenchmarkOnTickPatchState_NoChanges(b *testing.B) {
	b.Run("peers=8", func(b *testing.B) {
		h := newBenchHarness(b, domain.RoomID(2), 16)
		peers := make([]*benchCapturePeer, 8)
		for i := range peers {
			peers[i] = newBenchCapturePeer(domain.PeerID(i+1), "peer")
		}
		mustJoinBench(b, h.room, peers...)
		if err := h.policy.OnTickPatchState(); err != nil {
			b.Fatal(err)
		}
		for _, p := range peers {
			p.clear()
		}

		var sink error
		b.ResetTimer()
		for b.Loop() {
			sink = h.policy.OnTickPatchState()
		}
		_ = sink
	})
}
