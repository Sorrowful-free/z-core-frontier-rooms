package codec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

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
	value := make([]byte, valueLen)
	if _, err := io.ReadFull(buf, value); err != nil {
		return nil, err
	}
	valueState := state.ValueState(value)
	return &valueState, nil
}
