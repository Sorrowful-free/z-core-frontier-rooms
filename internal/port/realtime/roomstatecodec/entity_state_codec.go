package roomstatecodec

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"

type EntityStateCodec interface {
	FullState(state *state.EntityState) ([]byte, error)
	PatchState(oldState *state.EntityState, newState *state.EntityState) ([]byte, error)

	EncodeFullState(state *state.EntityState) ([]byte, error)
	ApplyPatch(oldState *state.EntityState, bytes []byte) (*state.EntityState, error)
}
