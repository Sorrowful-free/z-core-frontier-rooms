package state

import (
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type InputState struct {
	PeerID domain.PeerID
	Values MapState[ValueId, ValueState]
}

type InputStatePatch struct {
	PeerID domain.PeerID
	Values MapStatePatch[ValueId, ValueState, ValueState]
}

func (i *InputState) Equals(other *InputState, equals func(ValueState, ValueState) bool) bool {
	return i.PeerID == other.PeerID && i.Values.Equals(&other.Values, equals)
}

func (i *InputState) MakePatch(newInputState *InputState) (*InputStatePatch, error) {

	if !newInputState.PeerID.IsValid() || newInputState.PeerID != i.PeerID {
		return nil, fmt.Errorf("peer id mismatch: %d != %d", newInputState.PeerID, i.PeerID)
	}

	patch := &InputStatePatch{
		PeerID: i.PeerID,
		Values: *NewMapStatePatch[ValueId, ValueState, ValueState](),
	}
	patchValues, err := MakeMapStatePatch(&i.Values, &newInputState.Values, func(v1, v2 ValueState) bool {
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

func (i *InputState) ApplyPatch(patch InputStatePatch) error {
	if !patch.PeerID.IsValid() || patch.PeerID != i.PeerID {
		return fmt.Errorf("invalid peer id: %d", patch.PeerID)
	}
	err := ApplyMapStatePatch(&i.Values, patch.Values, func(v1, v2 ValueState) (*ValueState, error) {
		return &v2, nil
	})
	if err != nil {
		return err
	}
	return nil
}
