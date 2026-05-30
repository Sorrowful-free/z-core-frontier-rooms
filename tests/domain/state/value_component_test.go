package state_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func TestComponentState_patchSingleValue_roundTrip(t *testing.T) {
	t.Parallel()

	cur := &state.ComponentState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	cur.Values[state.ValueId(1)] = state.ValueState([]byte{1, 2})
	cur.Values[state.ValueId(2)] = state.ValueState([]byte{9})

	next := &state.ComponentState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	next.Values[state.ValueId(1)] = state.ValueState([]byte{1, 2, 3})
	next.Values[state.ValueId(2)] = state.ValueState([]byte{9})

	patch, err := cur.MakePatch(next)
	if err != nil {
		t.Fatalf("MakePatch: %v", err)
	}
	if patch == nil {
		t.Fatal("expected patch")
	}

	if err := cur.ApplyPatch(*patch); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}

	if !cur.Equals(next, func(a, b state.ValueState) bool { return a.Equals(&b) }) {
		t.Fatalf("state after apply = %#v, want %#v", cur.Values, next.Values)
	}
}

func TestComponentState_MakePatch_noChangesReturnsNil(t *testing.T) {
	t.Parallel()

	c := &state.ComponentState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	c.Values[state.ValueId(1)] = state.ValueState([]byte{7})

	other := &state.ComponentState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	other.Values[state.ValueId(1)] = state.ValueState([]byte{7})

	patch, err := c.MakePatch(other)
	if err != nil {
		t.Fatalf("MakePatch: %v", err)
	}
	if patch != nil {
		t.Fatalf("expected nil patch, got %#v", patch)
	}
}
