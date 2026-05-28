package roomstatecodec

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"

type RoomStateCodec interface {
	FullState(state *state.RoomState) ([]byte, error)
	PatchState(oldState *state.RoomState, newState *state.RoomState) ([]byte, error)

	EncodeFullState(state *state.RoomState) ([]byte, error)
	ApplyPatch(oldState *state.RoomState, bytes []byte) (*state.RoomState, error)
}
