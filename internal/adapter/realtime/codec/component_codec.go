package codec

import (
	"bytes"
	"encoding/binary"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

const (
	ComponentStateMaxValues = 32
)

func writeComponentID(buf *bytes.Buffer, id state.ComponentID) error {
	return binary.Write(buf, binary.BigEndian, id)
}

func readComponentID(buf *bytes.Buffer) (state.ComponentID, error) {
	var id state.ComponentID
	if err := binary.Read(buf, binary.BigEndian, &id); err != nil {
		return state.ComponentIDNone, err
	}
	return id, nil
}

func writeComponentState(buf *bytes.Buffer, component *state.ComponentState) error {
	return writeMapState(buf, &component.Values, func(k state.ValueId, buf *bytes.Buffer) error {
		return binary.Write(buf, binary.BigEndian, k)
	}, func(v *state.ValueState, buf *bytes.Buffer) error {
		return writeValueState(buf, v)
	}, ComponentStateMaxValues)
}

func readComponentState(buf *bytes.Buffer) (*state.ComponentState, error) {
	values, err := readMapState(buf, func(buf *bytes.Buffer) (state.ValueId, error) {
		var id state.ValueId
		if err := binary.Read(buf, binary.BigEndian, &id); err != nil {
			return 0, err
		}
		return id, nil
	}, func(buf *bytes.Buffer) (*state.ValueState, error) {
		value, err := readValueState(buf)
		if err != nil {
			return nil, err
		}
		return value, nil
	}, ComponentStateMaxValues)
	if err != nil {
		return nil, err
	}
	return &state.ComponentState{Values: *values}, nil
}

func writeComponentStatePatch(buf *bytes.Buffer, patch *state.ComponentStatePatch) error {
	return writeMapPatсhState(buf, &patch.Values, func(k state.ValueId, buf *bytes.Buffer) error {
		return binary.Write(buf, binary.BigEndian, k)
	}, func(v *state.ValueState, buf *bytes.Buffer) error {
		return writeValueState(buf, v)
	}, func(v *state.ValueState, buf *bytes.Buffer) error {
		return writeValueState(buf, v)
	}, ComponentStateMaxValues)
}

func readComponentStatePatch(buf *bytes.Buffer) (*state.ComponentStatePatch, error) {

	values, err := readMapPatchState(buf, func(buf *bytes.Buffer) (state.ValueId, error) {
		var id state.ValueId
		if err := binary.Read(buf, binary.BigEndian, &id); err != nil {
			return 0, err
		}
		return id, nil
	}, func(buf *bytes.Buffer) (*state.ValueState, error) {
		value, err := readValueState(buf)
		if err != nil {
			return nil, err
		}
		return value, nil
	}, func(buf *bytes.Buffer) (*state.ValueState, error) {
		value, err := readValueState(buf)
		if err != nil {
			return nil, err
		}
		return value, nil
	}, ComponentStateMaxValues)
	if err != nil {
		return nil, err
	}
	return &state.ComponentStatePatch{Values: *values}, nil
}
