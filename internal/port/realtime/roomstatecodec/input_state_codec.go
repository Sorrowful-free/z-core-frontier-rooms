package roomstatecodec

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"

type InputStateCodec interface {
	FullState(state *state.InputState) ([]byte, error)
	PatchState(oldState *state.InputState, newState *state.InputState) ([]byte, error)

	EncodeFullState(state *state.InputState) ([]byte, error)
	ApplyPatch(oldState *state.InputState, bytes []byte) (*state.InputState, error)
}
