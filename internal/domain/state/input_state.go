package state

type InputState struct {
	Values MapState[ValueId, ValueState]
}

type InputStatePatch struct {
	Values MapStatePatch[ValueId, ValueState, ValueState]
}

func (i *InputState) Equals(other *InputState, equals func(ValueState, ValueState) bool) bool {
	return i.Values.Equals(other.Values, equals)
}

func (i *InputState) MakePatch(newInputState *InputState) (*InputStatePatch, error) {

	patch := &InputStatePatch{
		Values: *NewMapStatePatch[ValueId, ValueState, ValueState](),
	}
	patchValues, err := MakeMapStatePatch(i.Values, newInputState.Values, func(v1, v2 ValueState) bool {
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

func (i InputState) Clone() InputState {
	return InputState{
		Values: CloneMapState(i.Values, func(v ValueState) ValueState { return v.Clone() }),
	}
}

func (i *InputState) ApplyPatch(patch InputStatePatch) error {
	err := ApplyMapStatePatch(i.Values, patch.Values, func(v1, v2 ValueState) (*ValueState, error) {
		return &v2, nil
	})
	if err != nil {
		return err
	}
	return nil
}
