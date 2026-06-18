package codec_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func benchRpcState() *state.RpcState {
	rpc := &state.RpcState{
		ID:     state.RpcID(3),
		Target: state.RpcTargetPeer,
		Values: state.NewMapState[state.ValueId, state.ValueState](),
		PeerID: domain.PeerID(42),
	}
	rpc.Values[state.ValueId(1)] = state.ValueState(make([]byte, 16))
	return rpc
}

func BenchmarkRpcStateCodec_Encode(b *testing.B) {
	c := &codec.RpcStateCodec{}
	rpc := benchRpcState()

	var sink []byte
	var err error
	b.ResetTimer()
	for b.Loop() {
		sink, err = c.Encode(rpc)
		if err != nil {
			b.Fatal(err)
		}
	}
	_ = sink
}

func BenchmarkRpcStateCodec_Decode(b *testing.B) {
	c := &codec.RpcStateCodec{}
	rpc := benchRpcState()
	data, err := c.Encode(rpc)
	if err != nil {
		b.Fatal(err)
	}

	var sink *state.RpcState
	b.ResetTimer()
	for b.Loop() {
		sink, err = c.Decode(data)
		if err != nil {
			b.Fatal(err)
		}
	}
	_ = sink
}
