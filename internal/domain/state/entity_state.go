package state

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type EntityID uint32

const (
	EntityIDNone EntityID = 0
)

func (e EntityID) IsValid() bool {
	return e != EntityIDNone
}

type EntityState struct {
	ID         EntityID
	Owner      domain.PeerID
	Components MapState[ComponentID, ComponentState]
}

type EntityStatePatch struct {
	ID         EntityID
	Owner      *domain.PeerID
	Components MapStatePatch[ComponentID, ComponentState, ComponentStatePatch]
}

func (e *EntityState) Equals(other *EntityState, equals func(ComponentState, ComponentState) bool) bool {
	return e.ID == other.ID && e.Owner == other.Owner && e.Components.Equals(&other.Components, equals)
}

func (e *EntityState) MakePatch(newEntityState *EntityState) (*EntityStatePatch, error) {
	if !newEntityState.ID.IsValid() || newEntityState.ID != e.ID {
		return nil, fmt.Errorf("entity id mismatch: %d != %d", newEntityState.ID, e.ID)
	}

	patch := &EntityStatePatch{
		ID:         e.ID,
		Components: *NewMapStatePatch[ComponentID, ComponentState, ComponentStatePatch](),
	}

	hasChanges := false

	if e.Owner != newEntityState.Owner {
		patch.Owner = &newEntityState.Owner
		hasChanges = true
	}

	patchComponents, err := MakeMapStatePatch(&e.Components, &newEntityState.Components, func(c1, c2 ComponentState) bool {
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

func (e *EntityState) ApplyPatch(patch EntityStatePatch) error {
	if !patch.ID.IsValid() || patch.ID != e.ID {
		return fmt.Errorf("invalid entity id: %d", patch.ID)
	}

	if patch.Owner != nil && *patch.Owner != e.Owner {
		e.Owner = *patch.Owner
	}

	err := ApplyMapStatePatch(&e.Components, patch.Components, func(c1 ComponentState, c2 ComponentStatePatch) (*ComponentState, error) {
		err := c1.ApplyPatch(c2)
		if err != nil {
			return nil, err
		}
		return &c1, nil
	})
	if err != nil {
		return err
	}
	return nil
}
