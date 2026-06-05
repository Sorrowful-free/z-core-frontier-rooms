package state

import (
	"fmt"
)

type MapState[K comparable, T any] map[K]T

type MapStatePatch[K comparable, S any, P any] struct {
	Added   map[K]S
	Updated map[K]P
	Removed []K
}

func (m MapState[K, T]) Equals(other MapState[K, T], equals func(T, T) bool) bool {
	if len(m) != len(other) {
		return false
	}
	for k, v := range m {
		if _, ok := other[k]; !ok {
			return false
		}
		if !equals(v, other[k]) {
			return false
		}
	}
	return true
}

func NewMapState[K comparable, S any]() MapState[K, S] {
	return make(MapState[K, S])
}

// CloneMapState copies map keys and clones each value with cloneItem.
func CloneMapState[K comparable, T any](m MapState[K, T], cloneItem func(T) T) MapState[K, T] {
	out := make(MapState[K, T], len(m))
	for k, v := range m {
		out[k] = cloneItem(v)
	}
	return out
}

func NewMapStatePatch[K comparable, S any, P any]() *MapStatePatch[K, S, P] {
	return &MapStatePatch[K, S, P]{
		Added:   make(map[K]S),
		Updated: make(map[K]P),
		Removed: make([]K, 0),
	}
}

func MakeMapStatePatch[K comparable, S any, P any](oldMapState MapState[K, S], newMapState MapState[K, S], equals func(S, S) bool, patch func(S, S) (*P, error)) (*MapStatePatch[K, S, P], error) {

	patchState := &MapStatePatch[K, S, P]{
		Added:   make(map[K]S),
		Updated: make(map[K]P),
		Removed: make([]K, 0),
	}
	hasChanges := false
	for k, v := range newMapState {
		if _, ok := oldMapState[k]; !ok {
			patchState.Added[k] = v
			hasChanges = true
		}
	}
	for k, v := range oldMapState {
		if _, ok := newMapState[k]; ok {
			if !equals(v, newMapState[k]) {
				p, err := patch(oldMapState[k], newMapState[k])
				if err != nil {
					return nil, err
				}
				if p == nil {
					return nil, fmt.Errorf("patch function returned nil for key %v", k)
				}
				patchState.Updated[k] = *p
				hasChanges = true
			}
		}
	}
	for k, _ := range oldMapState {
		if _, ok := newMapState[k]; !ok {
			patchState.Removed = append(patchState.Removed, k)
			hasChanges = true
		}
	}
	if !hasChanges {
		return nil, nil
	}
	return patchState, nil
}

func ApplyMapStatePatch[K comparable, S any, P any](targetState MapState[K, S], patchState MapStatePatch[K, S, P], apply func(S, P) (*S, error)) error {
	for k, v := range patchState.Added {
		if _, ok := targetState[k]; ok {
			return fmt.Errorf("key %v already exists in map", k)
		}
		targetState[k] = v
	}
	for k, v := range patchState.Updated {
		if _, ok := targetState[k]; !ok {
			return fmt.Errorf("key %v not found in map", k)
		}
		newItem, err := apply(targetState[k], v)
		if err != nil {
			return err
		}
		if newItem == nil {
			return fmt.Errorf("apply function returned nil for key %v", k)
		}
		targetState[k] = *newItem
	}
	for _, k := range patchState.Removed {
		if _, ok := targetState[k]; !ok {
			return fmt.Errorf("key %v not found in map", k)
		}
		delete(targetState, k)
	}

	return nil
}
