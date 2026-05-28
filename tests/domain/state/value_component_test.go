package state_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func TestComponentState_patchSingleValue_roundTrip(t *testing.T) {
	t.Parallel()

	cur := &state.ComponentState{
		ID: state.ComponentID(3),
		Values: *state.NewMapState[state.ValueId, state.ValueState](),
	}
	cur.Values.Items[state.ValueId(1)] = state.ValueState{ID: state.ValueId(1), Value: []byte{1, 2}}
	cur.Values.Items[state.ValueId(2)] = state.ValueState{ID: state.ValueId(2), Value: []byte{9}}

	next := &state.ComponentState{
		ID: state.ComponentID(3),
		Values: *state.NewMapState[state.ValueId, state.ValueState](),
	}
	next.Values.Items[state.ValueId(1)] = state.ValueState{ID: state.ValueId(1), Value: []byte{1, 2, 3}}
	next.Values.Items[state.ValueId(2)] = state.ValueState{ID: state.ValueId(2), Value: []byte{9}}

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
		t.Fatalf("state after apply = %#v, want %#v", cur.Values.Items, next.Values.Items)
	}
}

func TestComponentState_MakePatch_noChangesReturnsNil(t *testing.T) {
	t.Parallel()

	c := &state.ComponentState{
		ID:     state.ComponentID(1),
		Values: *state.NewMapState[state.ValueId, state.ValueState](),
	}
	c.Values.Items[state.ValueId(1)] = state.ValueState{ID: state.ValueId(1), Value: []byte{7}}

	other := &state.ComponentState{
		ID:     state.ComponentID(1),
		Values: *state.NewMapState[state.ValueId, state.ValueState](),
	}
	other.Values.Items[state.ValueId(1)] = state.ValueState{ID: state.ValueId(1), Value: []byte{7}}

	patch, err := c.MakePatch(other)
	if err != nil {
		t.Fatalf("MakePatch: %v", err)
	}
	if patch != nil {
		t.Fatalf("expected nil patch, got %#v", patch)
	}
}
