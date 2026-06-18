package codec_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	benchfixtures "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/bench"
)

func buildCodecRoom(peerCount int) *state.RoomState {
	return benchfixtures.BuildRoomState(peerCount)
}

func benchRoomEntityAddPatch(b *testing.B, peerCount int) *state.RoomStatePatch {
	b.Helper()

	cur := buildCodecRoom(peerCount)
	next := cur.Clone()
	next.Entities[state.EntityID(10)] = state.EntityState{
		Owner:      domain.PeerID(1),
		Components: state.NewMapState[state.ComponentID, state.ComponentState](),
	}
	patch, err := cur.MakePatch(next)
	if err != nil {
		b.Fatal(err)
	}
	if patch == nil {
		b.Fatal("expected entity add patch")
	}
	return patch
}

func BenchmarkRoomStateCodec_Encode(b *testing.B) {
	c := newRoomCodec()
	for _, peers := range []int{0, 8, 64} {
		b.Run(benchfixtures.PeerCountLabel(peers), func(b *testing.B) {
			room := buildCodecRoom(peers)
			var sink []byte
			var err error
			b.ResetTimer()
			for b.Loop() {
				sink, err = c.Encode(room)
				if err != nil {
					b.Fatal(err)
				}
			}
			_ = sink
		})
	}
}

func BenchmarkRoomStateCodec_Decode(b *testing.B) {
	c := newRoomCodec()
	for _, peers := range []int{0, 8, 64} {
		b.Run(benchfixtures.PeerCountLabel(peers), func(b *testing.B) {
			room := buildCodecRoom(peers)
			data, err := c.Encode(room)
			if err != nil {
				b.Fatal(err)
			}

			var sink *state.RoomState
			b.ResetTimer()
			for b.Loop() {
				sink, err = c.Decode(data)
				if err != nil {
					b.Fatal(err)
				}
			}
			_ = sink
		})
	}
}

func BenchmarkRoomStateCodec_EncodePatch(b *testing.B) {
	c := newRoomCodec()

	b.Run("ping_refresh/peers=8", func(b *testing.B) {
		cur := buildCodecRoom(8)
		next := buildCodecRoom(8)
		for id, peer := range next.Peers {
			peer.Ping++
			next.Peers[id] = peer
		}
		patch, err := cur.MakePatch(next)
		if err != nil {
			b.Fatal(err)
		}

		var sink []byte
		b.ResetTimer()
		for b.Loop() {
			sink, err = c.EncodePatch(patch)
			if err != nil {
				b.Fatal(err)
			}
		}
		_ = sink
	})

	b.Run("add_entity/peers=8", func(b *testing.B) {
		patch := benchRoomEntityAddPatch(b, 8)
		var sink []byte
		var err error
		b.ResetTimer()
		for b.Loop() {
			sink, err = c.EncodePatch(patch)
			if err != nil {
				b.Fatal(err)
			}
		}
		_ = sink
	})
}

func BenchmarkRoomStateCodec_DecodePatch(b *testing.B) {
	c := newRoomCodec()

	b.Run("ping_refresh/peers=8", func(b *testing.B) {
		cur := buildCodecRoom(8)
		next := buildCodecRoom(8)
		for id, peer := range next.Peers {
			peer.Ping++
			next.Peers[id] = peer
		}
		patch, err := cur.MakePatch(next)
		if err != nil {
			b.Fatal(err)
		}
		data, err := c.EncodePatch(patch)
		if err != nil {
			b.Fatal(err)
		}

		var sink *state.RoomStatePatch
		b.ResetTimer()
		for b.Loop() {
			sink, err = c.DecodePatch(data)
			if err != nil {
				b.Fatal(err)
			}
		}
		_ = sink
	})

	b.Run("add_entity/peers=8", func(b *testing.B) {
		patch := benchRoomEntityAddPatch(b, 8)
		data, err := c.EncodePatch(patch)
		if err != nil {
			b.Fatal(err)
		}

		var sink *state.RoomStatePatch
		b.ResetTimer()
		for b.Loop() {
			sink, err = c.DecodePatch(data)
			if err != nil {
				b.Fatal(err)
			}
		}
		_ = sink
	})
}
