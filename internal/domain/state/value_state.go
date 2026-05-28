package state

import "slices"

type ValueId byte

const (
	ValueIdNone ValueId = 0
)

func (f ValueId) IsValid() bool {
	return f != ValueIdNone
}

type ValueState struct {
	ID    ValueId
	Value []byte
}

func (v *ValueState) Equals(other *ValueState) bool {
	return v.ID == other.ID && slices.Equal(v.Value, other.Value)
}
