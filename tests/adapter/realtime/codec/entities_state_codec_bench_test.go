package codec_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func benchEntitiesFull() state.EntitiesState {
	entities := state.NewMapState[state.EntityID, state.EntityState]()
	components := state.NewMapState[state.ComponentID, state.ComponentState]()
	components[state.ComponentID(1)] = state.ComponentState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	components[state.ComponentID(1)].Values[state.ValueId(1)] = state.ValueState(make([]byte, 16))
	entities[state.EntityID(10)] = state.EntityState{
		Owner:      domain.PeerID(1),
		Components: components,
	}
	return entities
}

func BenchmarkEntitiesStateCodec_Encode(b *testing.B) {
	c := codec.NewEntitiesStateCodec()
	entities := benchEntitiesFull()

	var sink []byte
	var err error
	b.ResetTimer()
	for b.Loop() {
		sink, err = c.Encode(entities)
		if err != nil {
			b.Fatal(err)
		}
	}
	_ = sink
}

func BenchmarkEntitiesStateCodec_Decode(b *testing.B) {
	c := codec.NewEntitiesStateCodec()
	entities := benchEntitiesFull()
	data, err := c.Encode(entities)
	if err != nil {
		b.Fatal(err)
	}

	var sink state.EntitiesState
	b.ResetTimer()
	for b.Loop() {
		sink, err = c.Decode(data)
		if err != nil {
			b.Fatal(err)
		}
	}
	_ = sink
}

func BenchmarkEntitiesStateCodec_EncodePatch(b *testing.B) {
	c := codec.NewEntitiesStateCodec()
	patch := state.NewMapStatePatch[state.EntityID, state.EntityState, state.EntityStatePatch]()
	patch.Added[state.EntityID(11)] = state.EntityState{
		Owner:      domain.PeerID(1),
		Components: state.NewMapState[state.ComponentID, state.ComponentState](),
	}

	var sink []byte
	var err error
	b.ResetTimer()
	for b.Loop() {
		sink, err = c.EncodePatch(patch)
		if err != nil {
			b.Fatal(err)
		}
	}
	_ = sink
}
