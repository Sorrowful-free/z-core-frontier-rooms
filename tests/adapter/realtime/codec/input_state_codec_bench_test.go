package codec_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func BenchmarkInputStateCodec_EncodePatch(b *testing.B) {
	c := codec.NewInputStateCodec()
	cur := &state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
	cur.Values[state.ValueId(1)] = state.ValueState(make([]byte, 16))
	next := &state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
	next.Values[state.ValueId(1)] = state.ValueState(append([]byte(nil), cur.Values[state.ValueId(1)]...))
	next.Values[state.ValueId(1)][0] = 0xFF

	patch, err := cur.MakePatch(next)
	if err != nil {
		b.Fatal(err)
	}

	var sink []byte
	b.ResetTimer()
	for b.Loop() {
		sink, err = c.EncodePatch(patch)
		if err != nil {
			b.Fatal(err)
		}
	}
	_ = sink
}

func BenchmarkInputStateCodec_DecodePatch(b *testing.B) {
	c := codec.NewInputStateCodec()
	cur := &state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
	cur.Values[state.ValueId(1)] = state.ValueState(make([]byte, 16))
	next := &state.InputState{Values: state.NewMapState[state.ValueId, state.ValueState]()}
	next.Values[state.ValueId(1)] = state.ValueState(append([]byte(nil), cur.Values[state.ValueId(1)]...))
	next.Values[state.ValueId(1)][0] = 0xFF

	patch, err := cur.MakePatch(next)
	if err != nil {
		b.Fatal(err)
	}
	data, err := c.EncodePatch(patch)
	if err != nil {
		b.Fatal(err)
	}

	var sink *state.InputStatePatch
	b.ResetTimer()
	for b.Loop() {
		sink, err = c.DecodePatch(data)
		if err != nil {
			b.Fatal(err)
		}
	}
	_ = sink
}
