package roomstatecodec

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"

type RpcStateCodec interface {
	Encode(rpc *state.RpcState) ([]byte, error)
	Decode(data []byte) (*state.RpcState, error)
}
