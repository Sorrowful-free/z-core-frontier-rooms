package codec_test

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func newRoomCodec() *codec.RoomStateCodec {
	return codec.NewRoomStateCodec()
}

func valueEqual(a, b state.ValueState) bool {
	return a.Equals(&b)
}

func inputEqual(a, b *state.InputState) bool {
	return a.Equals(b, valueEqual)
}
