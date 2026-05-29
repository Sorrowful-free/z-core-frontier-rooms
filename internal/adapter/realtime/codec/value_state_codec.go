package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

func writeValueID(buf *bytes.Buffer, id state.ValueId) error {
	return binary.Write(buf, binary.BigEndian, id)
}

func readValueID(buf *bytes.Buffer) (state.ValueId, error) {
	var id state.ValueId
	if err := binary.Read(buf, binary.BigEndian, &id); err != nil {
		return state.ValueIdNone, err
	}
	return id, nil
}

func writeValueState(buf *bytes.Buffer, v *state.ValueState) error {
	valueLen := len(*v)
	if valueLen > math.MaxUint8 {
		return fmt.Errorf("value length too large: %d", valueLen)
	}
	if err := binary.Write(buf, binary.BigEndian, byte(valueLen)); err != nil {
		return err
	}
	if _, err := buf.Write(*v); err != nil {
		return err
	}
	return nil
}

func readValueState(buf *bytes.Buffer) (*state.ValueState, error) {
	valueLen := byte(0)
	if err := binary.Read(buf, binary.BigEndian, &valueLen); err != nil {
		return nil, err
	}
	if valueLen > math.MaxUint8 {
		return nil, fmt.Errorf("value length too large: %d", valueLen)
	}
	value := make([]byte, valueLen)
	if _, err := buf.Read(value); err != nil {
		return nil, err
	}
	valueState := state.ValueState(value)
	return &valueState, nil
}
