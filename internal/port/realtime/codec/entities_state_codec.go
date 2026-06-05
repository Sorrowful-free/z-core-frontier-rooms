package codec

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"

type EntitiesStateCodec interface {
	Encode(state state.EntitiesState) ([]byte, error)
	Decode(data []byte) (state.EntitiesState, error)

	EncodePatch(patch *state.EntitiesStatePatch) ([]byte, error)
	DecodePatch(data []byte) (*state.EntitiesStatePatch, error)
}
