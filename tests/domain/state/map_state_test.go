package state_test

import (
	"testing"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func TestMapStateEquals_sameKeysAndValues(t *testing.T) {
	t.Parallel()

	left := state.NewMapState[int, string]()
	left[1] = "a"
	left[2] = "b"

	right := state.NewMapState[int, string]()
	right[1] = "a"
	right[2] = "b"

	if !left.Equals(right, func(a, b string) bool { return a == b }) {
		t.Fatal("expected maps to be equal")
	}
}

func TestMapStateEquals_differentKeySet(t *testing.T) {
	t.Parallel()

	left := state.NewMapState[int, string]()
	left[1] = "a"

	right := state.NewMapState[int, string]()
	right[2] = "a"

	if left.Equals(right, func(a, b string) bool { return a == b }) {
		t.Fatal("expected maps with different keys to differ")
	}
}

func TestMakeMapStatePatch_addUpdateRemove(t *testing.T) {
	t.Parallel()

	old := state.NewMapState[int, int]()
	old[1] = 10
	old[2] = 20

	newMap := state.NewMapState[int, int]()
	newMap[1] = 11 // update
	newMap[3] = 30 // add
	// key 2 removed

	patch, err := state.MakeMapStatePatch(old, newMap, func(a, b int) bool { return a == b }, func(_, b int) (*int, error) {
		v := b
		return &v, nil
	})
	if err != nil {
		t.Fatalf("MakeMapStatePatch: %v", err)
	}
	if patch == nil {
		t.Fatal("expected non-nil patch")
	}

	if len(patch.Added) != 1 || patch.Added[3] != 30 {
		t.Fatalf("Added = %#v, want key 3 -> 30", patch.Added)
	}
	if len(patch.Updated) != 1 || patch.Updated[1] != 11 {
		t.Fatalf("Updated = %#v, want key 1 -> 11", patch.Updated)
	}
	if len(patch.Removed) != 1 || patch.Removed[0] != 2 {
		t.Fatalf("Removed = %#v, want [2]", patch.Removed)
	}
}

func TestMakeMapStatePatch_noChangesReturnsNil(t *testing.T) {
	t.Parallel()

	m := state.NewMapState[int, int]()
	m[1] = 42

	other := state.NewMapState[int, int]()
	other[1] = 42

	patch, err := state.MakeMapStatePatch(m, other, func(a, b int) bool { return a == b }, func(_, b int) (*int, error) {
		v := b
		return &v, nil
	})
	if err != nil {
		t.Fatalf("MakeMapStatePatch: %v", err)
	}
	if patch != nil {
		t.Fatalf("expected nil patch, got %#v", patch)
	}
}

func TestApplyMapStatePatch_roundTrip(t *testing.T) {
	t.Parallel()

	target := state.NewMapState[int, int]()
	target[1] = 10

	patch := state.MapStatePatch[int, int, int]{
		Added:   map[int]int{2: 20},
		Updated: map[int]int{1: 15},
		Removed: []int{},
	}

	if err := state.ApplyMapStatePatch(target, patch, func(cur, p int) (*int, error) {
		v := p
		return &v, nil
	}); err != nil {
		t.Fatalf("ApplyMapStatePatch: %v", err)
	}

	if target[1] != 15 || target[2] != 20 {
		t.Fatalf("map = %#v, want 1:15 and 2:20", target)
	}
}

// Soft apply (P34): Added on an existing key is an upsert, not an error.
// A replica may run ahead of the delta stream (full state to the joiner +
// tick echo).
func TestApplyMapStatePatch_addedExistingKey_upserts(t *testing.T) {
	t.Parallel()

	target := state.NewMapState[int, int]()
	target[1] = 10

	patch := state.MapStatePatch[int, int, int]{
		Added: map[int]int{1: 99, 2: 20},
	}
	if err := state.ApplyMapStatePatch(target, patch, func(cur, p int) (*int, error) {
		v := p
		return &v, nil
	}); err != nil {
		t.Fatalf("ApplyMapStatePatch: %v", err)
	}
	if target[1] != 99 || target[2] != 20 {
		t.Fatalf("map = %#v, want upserted 1:99 and added 2:20", target)
	}
}

// Removed on a missing key — ignored, not an error.
func TestApplyMapStatePatch_removedMissingKey_ignored(t *testing.T) {
	t.Parallel()

	target := state.NewMapState[int, int]()
	target[1] = 10

	patch := state.MapStatePatch[int, int, int]{
		Removed: []int{7},
	}
	if err := state.ApplyMapStatePatch(target, patch, func(cur, p int) (*int, error) {
		v := p
		return &v, nil
	}); err != nil {
		t.Fatalf("ApplyMapStatePatch: %v", err)
	}
	if target[1] != 10 || len(target) != 1 {
		t.Fatalf("map = %#v, want unchanged {1:10}", target)
	}
}

// Updated on a missing key — still an error: in a correct stream the key
// must exist in both prev and new, meaning it exists on every replica; its
// absence is a desync detector.
func TestApplyMapStatePatch_updatedMissingKey_errors(t *testing.T) {
	t.Parallel()

	target := state.NewMapState[int, int]()
	target[1] = 10

	patch := state.MapStatePatch[int, int, int]{
		Updated: map[int]int{7: 77},
	}
	if err := state.ApplyMapStatePatch(target, patch, func(cur, p int) (*int, error) {
		v := p
		return &v, nil
	}); err == nil {
		t.Fatal("expected error for updated on missing key")
	}
	if target[1] != 10 || len(target) != 1 {
		t.Fatalf("map = %#v, want unchanged {1:10}", target)
	}
}

func TestApplyMapStatePatchCopy_errorPreservesSource(t *testing.T) {
	t.Parallel()

	source := state.NewMapState[int, int]()
	source[1] = 10
	source[2] = 20

	patch := state.MapStatePatch[int, int, int]{
		Updated: map[int]int{1: 11, 2: 22},
	}

	_, err := state.ApplyMapStatePatchCopy(source, patch, func(v int) int { return v }, func(cur, p int) (*int, error) {
		if p == 22 {
			return nil, errApplyFail
		}
		v := p
		return &v, nil
	})
	if err == nil {
		t.Fatal("expected apply error")
	}
	if source[1] != 10 || source[2] != 20 {
		t.Fatalf("source mutated on failed apply: %#v", source)
	}
}

var errApplyFail = &applyError{}

type applyError struct{}

func (e *applyError) Error() string { return "apply failed" }
