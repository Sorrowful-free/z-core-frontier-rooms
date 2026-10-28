package state_test

import (
	"fmt"
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	benchfixtures "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/bench"
	testpeer "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/peer"
)

func benchJoinPeers(b *testing.B, h *benchHarness, peerCount int) []*testpeer.Peer {
	b.Helper()
	peers := make([]*testpeer.Peer, peerCount)
	for i := range peers {
		peers[i] = testpeer.New(domain.PeerID(i+1), "peer", testpeer.WithoutCapture())
	}
	mustJoinBench(b, h.room, peers...)
	return peers
}

// benchSeedEntities — entities via the master's FullInput path: the state
// fills up as in production (the diff cost depends on |state|).
func benchSeedEntities(b *testing.B, h *benchHarness, master *testpeer.Peer, n int) {
	b.Helper()
	entities := state.NewMapState[state.EntityID, state.EntityState]()
	for i := 1; i <= n; i++ {
		components := state.NewMapState[state.ComponentID, state.ComponentState]()
		for c := 1; c <= 2; c++ {
			values := state.NewMapState[state.ValueId, state.ValueState]()
			for v := 1; v <= 4; v++ {
				values[state.ValueId(v)] = state.ValueState([]byte{byte(i), byte(c), byte(v), 1, 2, 3, 4, 5})
			}
			components[state.ComponentID(c)] = state.ComponentState{Values: values}
		}
		entities[state.EntityID(i)] = state.EntityState{
			EntityTypeID: state.EntityTypeID(1),
			Owner:        master.GetID(),
			Components:   components,
		}
	}
	payload, err := h.entitiesCodec.Encode(entities)
	if err != nil {
		b.Fatalf("Encode entities: %v", err)
	}
	if err := h.policy.OnMessage(events.RoomEvent{
		PeerID: master.GetID(),
		Frame:  domain.Frame{OpCode: state.OpCodeFullEntities, Payload: payload},
	}); err != nil {
		b.Fatalf("seed full entities: %v", err)
	}
}

// Dirty tick (tick-coalesced emission, P34): each iteration one input
// mutation (outside the timer — it is not the measured object) → the tick
// does diff + encode + broadcast. The entities scenario shows the diff
// cost growing with |state|.
func BenchmarkOnTickPatchState(b *testing.B) {
	for _, peerCount := range []int{8, 64} {
		for _, entCount := range []int{0, 200} {
			b.Run(fmt.Sprintf("%s/entities=%d", benchfixtures.PeerCountLabel(peerCount), entCount), func(b *testing.B) {
				h := newBenchHarness(b, domain.RoomID(1), 128)
				peers := benchJoinPeers(b, h, peerCount)
				if entCount > 0 {
					benchSeedEntities(b, h, peers[0], entCount)
				}
				benchSeedInput(b, h, peers[1].GetID())

				payloads := [2][]byte{
					benchEncodeInputPatch(b, h.inputCodec, 0xA1),
					benchEncodeInputPatch(b, h.inputCodec, 0xB2),
				}
				evs := [2]events.RoomEvent{
					{PeerID: peers[1].GetID(), Frame: domain.Frame{OpCode: state.OpCodePatchInput, Payload: payloads[0]}},
					{PeerID: peers[1].GetID(), Frame: domain.Frame{OpCode: state.OpCodePatchInput, Payload: payloads[1]}},
				}

				if err := h.policy.OnTickPatchState(); err != nil {
					b.Fatal(err)
				}

				var sink error
				i := 0
				b.ResetTimer()
				for b.Loop() {
					i++
					b.StopTimer()
					if err := h.policy.OnMessage(evs[i&1]); err != nil {
						b.Fatal(err)
					}
					b.StartTimer()
					sink = h.policy.OnTickPatchState()
				}
				_ = sink
			})
		}
	}
}

func BenchmarkOnTickPatchState_NoChanges(b *testing.B) {
	for _, peerCount := range []int{8, 64} {
		b.Run(benchfixtures.PeerCountLabel(peerCount), func(b *testing.B) {
			h := newBenchHarness(b, domain.RoomID(2), 128)
			benchJoinPeers(b, h, peerCount)
			// Converge: the first tick after the join flushes the accumulated join delta.
			if err := h.policy.OnTickPatchState(); err != nil {
				b.Fatal(err)
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
