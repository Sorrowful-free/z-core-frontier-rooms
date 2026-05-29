package codec

import (
	"bytes"
	"encoding/binary"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

type InputStateCodec struct {
}

func NewInputStateCodec() *InputStateCodec {
	return &InputStateCodec{}
}

func (c *InputStateCodec) Encode(input *state.InputState) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 128))

	if err := writeMapState(buf, &input.Values, func(k state.ValueId, buf *bytes.Buffer) error {
		return binary.Write(buf, binary.BigEndian, k)
	}, func(v state.ValueState, buf *bytes.Buffer) error {
		return writeValueState(buf, &v)
	}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *InputStateCodec) Decode(data []byte) (*state.InputState, error) {
	buf := bytes.NewBuffer(data)
	values, err := readMapState(buf, func(buf *bytes.Buffer) (state.ValueId, error) {
		var id state.ValueId
		if err := binary.Read(buf, binary.BigEndian, &id); err != nil {
			return 0, err
		}
		return id, nil
	}, func(buf *bytes.Buffer) (state.ValueState, error) {
		value, err := readValueState(buf)
		if err != nil {
			return nil, err
		}
		return *value, nil
	})
	if err != nil {
		return nil, err
	}
	return &state.InputState{Values: *values}, nil
}

func (c *InputStateCodec) EncodePatch(patch *state.InputStatePatch) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 128))
	if err := writeMapPatсhState(buf, &patch.Values, func(k state.ValueId, buf *bytes.Buffer) error {
		return binary.Write(buf, binary.BigEndian, k)
	}, func(v state.ValueState, buf *bytes.Buffer) error {
		return writeValueState(buf, &v)
	}, func(v state.ValueState, buf *bytes.Buffer) error {
		return writeValueState(buf, &v)
	}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *InputStateCodec) DecodePatch(data []byte) (*state.InputStatePatch, error) {
	buf := bytes.NewBuffer(data)
	values, err := readMapPathState(buf, func(buf *bytes.Buffer) (state.ValueId, error) {
		var id state.ValueId
		if err := binary.Read(buf, binary.BigEndian, &id); err != nil {
			return 0, err
		}
		return id, nil
	}, func(buf *bytes.Buffer) (state.ValueState, error) {
		value, err := readValueState(buf)
		if err != nil {
			return nil, err
		}
		return *value, nil
	}, func(buf *bytes.Buffer) (state.ValueState, error) {
		patch, err := readValueState(buf)
		if err != nil {
			return nil, err
		}
		return *patch, nil
	})
	if err != nil {
		return nil, err
	}
	return &state.InputStatePatch{Values: *values}, nil
}
