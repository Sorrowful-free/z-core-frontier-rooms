package roomstatecodec

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"

type ValueStateCodec interface {
	Encode(value *state.ValueState) ([]byte, error)
	Decode(data []byte) (*state.ValueState, error)
}
