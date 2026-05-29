package state

import (
	"fmt"
)

type MapState[K comparable, T any] struct {
	Items map[K]T
}

type MapStatePatch[K comparable, S any, P any] struct {
	Added   map[K]S
	Updated map[K]P
	Removed []K
}

func (m *MapState[K, T]) Equals(other *MapState[K, T], equals func(T, T) bool) bool {
	if len(m.Items) != len(other.Items) {
		return false
	}
	for k, v := range m.Items {
		if _, ok := other.Items[k]; !ok {
			return false
		}
		if !equals(v, other.Items[k]) {
			return false
		}
	}
	return true
}

func NewMapState[K comparable, S any]() *MapState[K, S] {
	return &MapState[K, S]{
		Items: make(map[K]S),
	}
}

func NewMapStatePatch[K comparable, S any, P any]() *MapStatePatch[K, S, P] {
	return &MapStatePatch[K, S, P]{
		Added:   make(map[K]S),
		Updated: make(map[K]P),
		Removed: make([]K, 0),
	}
}

func MakeMapStatePatch[K comparable, S any, P any](oldMapState *MapState[K, S], newMapState *MapState[K, S], equals func(S, S) bool, patch func(S, S) (*P, error)) (*MapStatePatch[K, S, P], error) {

	patchState := &MapStatePatch[K, S, P]{
		Added:   make(map[K]S),
		Updated: make(map[K]P),
		Removed: make([]K, 0),
	}
	hasChanges := false
	for k, v := range newMapState.Items {
		if _, ok := oldMapState.Items[k]; !ok {
			patchState.Added[k] = v
			hasChanges = true
		}
	}
	for k, v := range oldMapState.Items {
		if _, ok := newMapState.Items[k]; ok {
			if !equals(v, newMapState.Items[k]) {
				p, err := patch(oldMapState.Items[k], newMapState.Items[k])
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
	for k, _ := range oldMapState.Items {
		if _, ok := newMapState.Items[k]; !ok {
			patchState.Removed = append(patchState.Removed, k)
			hasChanges = true
		}
	}
	if !hasChanges {
		return nil, nil
	}
	return patchState, nil
}

func ApplyMapStatePatch[K comparable, S any, P any](targetState *MapState[K, S], patchState MapStatePatch[K, S, P], apply func(S, P) (*S, error)) error {
	for k, v := range patchState.Added {
		if _, ok := targetState.Items[k]; ok {
			return fmt.Errorf("key %v already exists in map", k)
		}
		targetState.Items[k] = v
	}
	for k, v := range patchState.Updated {
		if _, ok := targetState.Items[k]; !ok {
			return fmt.Errorf("key %v not found in map", k)
		}
		newItem, err := apply(targetState.Items[k], v)
		if err != nil {
			return err
		}
		if newItem == nil {
			return fmt.Errorf("apply function returned nil for key %v", k)
		}
		targetState.Items[k] = *newItem
	}
	for _, k := range patchState.Removed {
		if _, ok := targetState.Items[k]; !ok {
			return fmt.Errorf("key %v not found in map", k)
		}
		delete(targetState.Items, k)
	}

	return nil
}
