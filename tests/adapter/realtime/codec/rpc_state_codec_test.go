package codec_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func TestRpcStateCodec_roundTrip_withPeerID(t *testing.T) {
	t.Parallel()

	c := &codec.RpcStateCodec{}
	rpc := &state.RpcState{
		ID:     state.RpcID(3),
		Target: state.RpcTargetPeer,
		Values: *state.NewMapState[state.ValueId, state.ValueState](),
		PeerID: domain.PeerID(42),
	}
	rpc.Values.Items[state.ValueId(1)] = state.ValueState([]byte{1, 2})

	data, err := c.Encode(rpc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := c.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.ID != rpc.ID || got.Target != rpc.Target || got.PeerID != rpc.PeerID {
		t.Fatalf("header: got id=%d target=%d peer=%d", got.ID, got.Target, got.PeerID)
	}
	wantV := rpc.Values.Items[state.ValueId(1)]
	gotV := got.Values.Items[state.ValueId(1)]
	if len(got.Values.Items) != 1 || !gotV.Equals(&wantV) {
		t.Fatalf("values: %#v", got.Values.Items)
	}
}

func TestRpcStateCodec_roundTrip_peerIDZero(t *testing.T) {
	t.Parallel()

	c := &codec.RpcStateCodec{}
	rpc := &state.RpcState{
		ID:     state.RpcID(1),
		Target: state.RpcTargetMaster,
		Values: *state.NewMapState[state.ValueId, state.ValueState](),
		PeerID: domain.PeerID(0),
	}

	data, err := c.Encode(rpc)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := c.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.PeerID != domain.PeerID(0) {
		t.Fatalf("PeerID = %d, want 0", got.PeerID)
	}
}
