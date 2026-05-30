package state_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func TestInputState_valuePatch_roundTrip(t *testing.T) {
	t.Parallel()

	cur := &state.InputState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	cur.Values[state.ValueId(1)] = state.ValueState([]byte{1})

	next := &state.InputState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	next.Values[state.ValueId(1)] = state.ValueState([]byte{1, 2})

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
		t.Fatalf("input after apply = %#v, want %#v", cur.Values, next.Values)
	}
}

func TestEntityState_ownerAndComponentValue_roundTrip(t *testing.T) {
	t.Parallel()

	cur := &state.EntityState{
		Owner:      domain.PeerID(1),
		Components: state.NewMapState[state.ComponentID, state.ComponentState](),
	}
	cur.Components[state.ComponentID(3)] = state.ComponentState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	cur.Components[state.ComponentID(3)].Values[state.ValueId(1)] =
		state.ValueState([]byte{0})

	next := &state.EntityState{
		Owner:      domain.PeerID(2),
		Components: state.NewMapState[state.ComponentID, state.ComponentState](),
	}
	next.Components[state.ComponentID(3)] = state.ComponentState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	next.Components[state.ComponentID(3)].Values[state.ValueId(1)] =
		state.ValueState([]byte{1})

	patch, err := cur.MakePatch(next)
	if err != nil {
		t.Fatalf("MakePatch: %v", err)
	}
	if patch == nil {
		t.Fatal("expected patch")
	}
	if patch.Owner == nil || *patch.Owner != domain.PeerID(2) {
		t.Fatalf("Owner patch = %v, want peer 2", patch.Owner)
	}

	if err := cur.ApplyPatch(*patch); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}

	if !cur.Equals(next, func(c1, c2 state.ComponentState) bool {
		return c1.Equals(&c2, func(v1, v2 state.ValueState) bool { return v1.Equals(&v2) })
	}) {
		t.Fatalf("entity after apply = owner %d components %#v", cur.Owner, cur.Components)
	}
}
