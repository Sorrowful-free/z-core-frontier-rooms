package state

type ComponentID byte

const (
	ComponentIDNone ComponentID = 0
)

type ComponentState struct {
	Values MapState[ValueId, ValueState]
}

type ComponentStatePatch struct {
	Values MapStatePatch[ValueId, ValueState, ValueState]
}

func (c ComponentID) IsValid() bool {
	return c != ComponentIDNone
}

func (c *ComponentState) Equals(other *ComponentState, equals func(ValueState, ValueState) bool) bool {
	return c.Values.Equals(other.Values, equals)
}

func (c *ComponentState) MakePatch(newComponentState *ComponentState) (*ComponentStatePatch, error) {
	patch := &ComponentStatePatch{
		Values: *NewMapStatePatch[ValueId, ValueState, ValueState](),
	}

	patchValues, err := MakeMapStatePatch(c.Values, newComponentState.Values, func(v1, v2 ValueState) bool {
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

func (c ComponentState) Clone() ComponentState {
	return ComponentState{
		Values: CloneMapState(c.Values, func(v ValueState) ValueState { return v.Clone() }),
	}
}

func (c *ComponentState) ApplyPatch(patch ComponentStatePatch) error {
	nextValues, err := ApplyMapStatePatchCopy(c.Values, patch.Values, func(v ValueState) ValueState {
		return v.Clone()
	}, func(v1, v2 ValueState) (*ValueState, error) {
		return &v2, nil
	})
	if err != nil {
		return err
	}
	c.Values = nextValues
	return nil
}
