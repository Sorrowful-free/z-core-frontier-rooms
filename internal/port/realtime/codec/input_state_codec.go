package codec

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"

type InputStateCodec interface {
	Encode(input *state.InputState) ([]byte, error)
	Decode(data []byte) (*state.InputState, error)

	EncodePatch(patch *state.InputStatePatch) ([]byte, error)
	DecodePatch(data []byte) (*state.InputStatePatch, error)
}
