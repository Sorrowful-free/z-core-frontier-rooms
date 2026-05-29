package codec

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"

type RoomStateCodec interface {
	Encode(state *state.RoomState) ([]byte, error)
	Decode(data []byte) (*state.RoomState, error)

	EncodePatch(patch *state.RoomStatePatch) ([]byte, error)
	DecodePatch(data []byte) (*state.RoomStatePatch, error)
}
