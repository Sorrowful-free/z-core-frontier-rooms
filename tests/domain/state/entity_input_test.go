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

func TestEntityState_ApplyPatch_errorPreservesOwnerAndComponents(t *testing.T) {
	t.Parallel()

	cur := &state.EntityState{
		Owner:      domain.PeerID(1),
		Components: state.NewMapState[state.ComponentID, state.ComponentState](),
	}
	cur.Components[state.ComponentID(1)] = state.ComponentState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	cur.Components[state.ComponentID(1)].Values[state.ValueId(1)] = state.ValueState([]byte{1})
	cur.Components[state.ComponentID(2)] = state.ComponentState{
		Values: state.NewMapState[state.ValueId, state.ValueState](),
	}
	cur.Components[state.ComponentID(2)].Values[state.ValueId(1)] = state.ValueState([]byte{2})
	before := cur.Clone()

	newOwner := domain.PeerID(9)
	patch := state.EntityStatePatch{
		Owner: &newOwner,
		Components: state.MapStatePatch[state.ComponentID, state.ComponentState, state.ComponentStatePatch]{
			Updated: map[state.ComponentID]state.ComponentStatePatch{
				state.ComponentID(1): {
					Values: state.MapStatePatch[state.ValueId, state.ValueState, state.ValueState]{
						Updated: map[state.ValueId]state.ValueState{
							state.ValueId(1): state.ValueState([]byte{9}),
						},
					},
				},
				state.ComponentID(99): {},
			},
		},
	}

	err := cur.ApplyPatch(patch)
	if err == nil {
		t.Fatal("expected apply error for missing component key")
	}
	if cur.Owner != before.Owner {
		t.Fatalf("owner = %d, want %d after failed apply", cur.Owner, before.Owner)
	}
	if !cur.Equals(&before, func(c1, c2 state.ComponentState) bool {
		return c1.Equals(&c2, func(v1, v2 state.ValueState) bool { return v1.Equals(&v2) })
	}) {
		t.Fatalf("entity mutated on failed apply: owner=%d components=%#v", cur.Owner, cur.Components)
	}
}

func TestEntityState_entityTypeID_roundTrip(t *testing.T) {
	t.Parallel()

	cur := &state.EntityState{
		EntityTypeID: state.EntityTypeID(1),
		Owner:        domain.PeerID(1),
		Components:   state.NewMapState[state.ComponentID, state.ComponentState](),
	}

	next := &state.EntityState{
		EntityTypeID: state.EntityTypeID(2),
		Owner:        domain.PeerID(1),
		Components:   state.NewMapState[state.ComponentID, state.ComponentState](),
	}

	patch, err := cur.MakePatch(next)
	if err != nil {
		t.Fatalf("MakePatch: %v", err)
	}
	if patch == nil {
		t.Fatal("expected patch")
	}
	if patch.EntityTypeID == nil || *patch.EntityTypeID != state.EntityTypeID(2) {
		t.Fatalf("EntityTypeID patch = %v, want 2", patch.EntityTypeID)
	}

	if err := cur.ApplyPatch(*patch); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}
	if cur.EntityTypeID != state.EntityTypeID(2) {
		t.Fatalf("entity_type_id = %d, want 2", cur.EntityTypeID)
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
