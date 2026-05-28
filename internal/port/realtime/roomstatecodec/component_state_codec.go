package roomstatecodec

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"

type ComponentStateCodec interface {
	FullState(state *state.ComponentState) ([]byte, error)
	PatchState(oldState *state.ComponentState, newState *state.ComponentState) ([]byte, error)

	EncodeFullState(state *state.ComponentState) ([]byte, error)
	ApplyPatch(oldState *state.ComponentState, bytes []byte) (*state.ComponentState, error)
}
