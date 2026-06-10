package state

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type EntityID uint16

const (
	EntityIDNone EntityID = 0
)

func (e EntityID) IsValid() bool {
	return e != EntityIDNone
}

type EntityState struct {
	Owner      domain.PeerID
	Components MapState[ComponentID, ComponentState]
}

type EntityStatePatch struct {
	Owner      *domain.PeerID
	Components MapStatePatch[ComponentID, ComponentState, ComponentStatePatch]
}

type EntitiesState = MapState[EntityID, EntityState]

type EntitiesStatePatch = MapStatePatch[EntityID, EntityState, EntityStatePatch]

func (e *EntityState) Equals(other *EntityState, equals func(ComponentState, ComponentState) bool) bool {
	return e.Owner == other.Owner && e.Components.Equals(other.Components, equals)
}

func (e *EntityState) MakePatch(newEntityState *EntityState) (*EntityStatePatch, error) {
	patch := &EntityStatePatch{
		Components: *NewMapStatePatch[ComponentID, ComponentState, ComponentStatePatch](),
	}

	hasChanges := false

	if e.Owner != newEntityState.Owner {
		patch.Owner = &newEntityState.Owner
		hasChanges = true
	}

	patchComponents, err := MakeMapStatePatch(e.Components, newEntityState.Components, func(c1, c2 ComponentState) bool {
		return c1.Equals(&c2, func(v1, v2 ValueState) bool {
			return v1.Equals(&v2)
		})
	}, func(a, b ComponentState) (*ComponentStatePatch, error) {
		aPatch, err := a.MakePatch(&b)
		if err != nil {
			return nil, err
		}
		return aPatch, nil
	})

	if err != nil {
		return nil, err
	}

	if patchComponents != nil {
		patch.Components = *patchComponents
		hasChanges = true
	}
	if !hasChanges {
		return nil, nil
	}

	return patch, nil
}

func (e EntityState) Clone() EntityState {
	return EntityState{
		Owner:      e.Owner,
		Components: CloneMapState(e.Components, func(c ComponentState) ComponentState { return c.Clone() }),
	}
}

func (e *EntityState) ApplyPatch(patch EntityStatePatch) error {
	nextComponents, err := ApplyMapStatePatchCopy(e.Components, patch.Components, func(c ComponentState) ComponentState {
		return c.Clone()
	}, func(c1 ComponentState, c2 ComponentStatePatch) (*ComponentState, error) {
		next := c1.Clone()
		if err := next.ApplyPatch(c2); err != nil {
			return nil, err
		}
		return &next, nil
	})
	if err != nil {
		return err
	}
	e.Components = nextComponents
	if patch.Owner != nil && *patch.Owner != e.Owner {
		e.Owner = *patch.Owner
	}
	return nil
}
