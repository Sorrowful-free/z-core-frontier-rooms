package state

import (
	"fmt"
)

type MapState[K comparable, T any] struct {
	Items map[K]T
}

type MapStatePatch[K comparable, T any, P any] struct {
	Added   map[K]T
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

func NewMapState[K comparable, T any]() *MapState[K, T] {
	return &MapState[K, T]{
		Items: make(map[K]T),
	}
}

func NewMapStatePatch[K comparable, T any, P any]() *MapStatePatch[K, T, P] {
	return &MapStatePatch[K, T, P]{
		Added:   make(map[K]T),
		Updated: make(map[K]P),
		Removed: make([]K, 0),
	}
}

func MakeMapStatePatch[K comparable, T any, P any](oldMapState *MapState[K, T], newMapState *MapState[K, T], equals func(T, T) bool, patch func(T, T) (*P, error)) (*MapStatePatch[K, T, P], error) {

	patchState := &MapStatePatch[K, T, P]{
		Added:   make(map[K]T),
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

func ApplyMapStatePatch[K comparable, T any, P any](targetState *MapState[K, T], patchState MapStatePatch[K, T, P], apply func(T, P) (*T, error)) error {
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
