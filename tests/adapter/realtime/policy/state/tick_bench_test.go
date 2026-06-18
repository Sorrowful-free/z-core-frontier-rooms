package state_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	benchfixtures "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/bench"
)

func benchJoinPeers(b *testing.B, h *benchHarness, peerCount int) []*benchCapturePeer {
	b.Helper()
	peers := make([]*benchCapturePeer, peerCount)
	for i := range peers {
		peers[i] = newBenchCapturePeer(domain.PeerID(i+1), "peer")
	}
	mustJoinBench(b, h.room, peers...)
	return peers
}

func BenchmarkOnTickPatchState(b *testing.B) {
	for _, peerCount := range []int{8, 64} {
		b.Run(benchfixtures.PeerCountLabel(peerCount), func(b *testing.B) {
			h := newBenchHarness(b, domain.RoomID(1), 128)
			peers := benchJoinPeers(b, h, peerCount)

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
}

func BenchmarkOnTickPatchState_NoChanges(b *testing.B) {
	for _, peerCount := range []int{8, 64} {
		b.Run(benchfixtures.PeerCountLabel(peerCount), func(b *testing.B) {
			h := newBenchHarness(b, domain.RoomID(2), 128)
			peers := benchJoinPeers(b, h, peerCount)
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
}
