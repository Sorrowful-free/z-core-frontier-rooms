package codec_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
	benchfixtures "github.com/Sorrowful-free/z-core-frontier-rooms/tests/testutil/bench"
)

func buildCodecRoom(peerCount int) *state.RoomState {
	return benchfixtures.BuildRoomState(peerCount)
}

func BenchmarkRoomStateCodec_Encode(b *testing.B) {
	c := newRoomCodec()
	for _, peers := range []int{0, 8} {
		b.Run(peersLabel(peers), func(b *testing.B) {
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
	for _, peers := range []int{0, 8} {
		b.Run(peersLabel(peers), func(b *testing.B) {
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
}

func peersLabel(n int) string {
	if n == 0 {
		return "peers=0"
	}
	return "peers=8"
}
