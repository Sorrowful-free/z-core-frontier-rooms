package roomstatecodec

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"

type PeerStateCodec interface {
	FullState(state *state.PeerState) ([]byte, error)
	PatchState(oldState *state.PeerState, newState *state.PeerState) ([]byte, error)

	EncodeFullState(state *state.PeerState) ([]byte, error)
	ApplyPatch(oldState *state.PeerState, bytes []byte) (*state.PeerState, error)
}
