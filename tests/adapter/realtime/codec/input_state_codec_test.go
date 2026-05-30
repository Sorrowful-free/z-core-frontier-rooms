package codec_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func TestInputStateCodec_roundTrip_full(t *testing.T) {
	t.Parallel()

	c := codec.NewInputStateCodec()
	in := &state.InputState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	in.Values[state.ValueId(1)] = state.ValueState([]byte{9, 8})
	in.Values[state.ValueId(2)] = state.ValueState([]byte{0xFF})

	data, err := c.Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := c.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !inputEqual(in, got) {
		t.Fatalf("got %#v, want %#v", got.Values, in.Values)
	}
}

func TestInputStateCodec_roundTrip_patch(t *testing.T) {
	t.Parallel()

	c := codec.NewInputStateCodec()
	cur := &state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
	cur.Values[state.ValueId(1)] = state.ValueState([]byte{1})
	next := &state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
	next.Values[state.ValueId(1)] = state.ValueState([]byte{1, 2, 3})
	next.Values[state.ValueId(3)] = state.ValueState([]byte{7})

	patch, err := cur.MakePatch(next)
	if err != nil {
		t.Fatalf("MakePatch: %v", err)
	}

	data, err := c.EncodePatch(patch)
	if err != nil {
		t.Fatalf("EncodePatch: %v", err)
	}
	gotPatch, err := c.DecodePatch(data)
	if err != nil {
		t.Fatalf("DecodePatch: %v", err)
	}
	if err := cur.ApplyPatch(*gotPatch); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}
	if !inputEqual(next, cur) {
		t.Fatalf("after apply: %#v", cur.Values)
	}
}

func TestInputStateCodec_decode_truncated(t *testing.T) {
	t.Parallel()

	c := codec.NewInputStateCodec()
	// count u16 BE = 1, no key/value follows
	_, err := c.Decode([]byte{0, 1})
	if err == nil {
		t.Fatal("expected error on truncated payload")
	}
}
