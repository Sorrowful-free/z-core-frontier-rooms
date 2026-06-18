package state_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	benchfixtures "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/bench"
)

func BenchmarkRoomState_MakePatch(b *testing.B) {
	for _, peers := range []int{0, 8} {
		b.Run(peersLabel(peers), func(b *testing.B) {
			current := benchfixtures.BuildRoomState(peers)
			prev := current.Clone()

			var sink *state.RoomStatePatch
			b.ResetTimer()
			for b.Loop() {
				var err error
				sink, err = prev.MakePatch(current)
				if err != nil {
					b.Fatal(err)
				}
			}
			_ = sink
		})
	}
}

func BenchmarkRoomState_ApplyPatch(b *testing.B) {
	b.Run(peersLabel(8), func(b *testing.B) {
		current := benchfixtures.BuildRoomState(8)
		prev := current.Clone()
		patch, err := prev.MakePatch(current)
		if err != nil {
			b.Fatal(err)
		}
		if patch == nil {
			cap8 := int8(8)
			patch = &state.RoomStatePatch{Capacity: &cap8}
		}

		base := state.NewRoomState(domain.RoomID(1), 4, "")
		var sink error
		b.ResetTimer()
		for b.Loop() {
			room := base.Clone()
			sink = room.ApplyPatch(*patch)
		}
		_ = sink
	})
}

func BenchmarkRoomState_Clone(b *testing.B) {
	b.Run(peersLabel(8), func(b *testing.B) {
		room := benchfixtures.BuildRoomState(8)

		var sink *state.RoomState
		b.ResetTimer()
		for b.Loop() {
			sink = room.Clone()
		}
		_ = sink
	})
}

func peersLabel(n int) string {
	if n == 0 {
		return "peers=0"
	}
	return "peers=8"
}
