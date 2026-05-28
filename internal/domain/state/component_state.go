package state

import (
	"fmt"
)

type ComponentID byte

const (
	ComponentIDNone ComponentID = 0
)

type ComponentState struct {
	ID     ComponentID
	Values MapState[ValueId, ValueState]
}

type ComponentStatePatch struct {
	ID     ComponentID
	Values MapStatePatch[ValueId, ValueState, ValueState]
}

func (c ComponentID) IsValid() bool {
	return c != ComponentIDNone
}

func (c *ComponentState) Equals(other *ComponentState, equals func(ValueState, ValueState) bool) bool {
	return c.ID == other.ID && c.Values.Equals(&other.Values, equals)
}

func (c *ComponentState) MakePatch(newComponentState *ComponentState) (*ComponentStatePatch, error) {
	if !newComponentState.ID.IsValid() || newComponentState.ID != c.ID {
		return nil, fmt.Errorf("component id mismatch: %d != %d", newComponentState.ID, c.ID)
	}

	patch := &ComponentStatePatch{
		ID:     c.ID,
		Values: *NewMapStatePatch[ValueId, ValueState, ValueState](),
	}

	patchValues, err := MakeMapStatePatch(&c.Values, &newComponentState.Values, func(v1, v2 ValueState) bool {
		return v1.Equals(&v2)
	}, func(v1, v2 ValueState) (*ValueState, error) {
		return &v2, nil
	})

	if err != nil {
		return nil, err
	}
	if patchValues == nil {
		return nil, nil
	}
	patch.Values = *patchValues
	return patch, nil
}

func (c *ComponentState) ApplyPatch(patch ComponentStatePatch) error {
	if !patch.ID.IsValid() || patch.ID != c.ID {
		return fmt.Errorf("invalid component id: %d", patch.ID)
	}

	err := ApplyMapStatePatch(&c.Values, patch.Values, func(v1, v2 ValueState) (*ValueState, error) {
		return &v2, nil
	})
	if err != nil {
		return err
	}

	return nil
}
