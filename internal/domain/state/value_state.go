package state

import "slices"

type ValueId byte

const (
	ValueIdNone ValueId = 0
)

func (f ValueId) IsValid() bool {
	return f != ValueIdNone
}

type ValueState []byte

func (v *ValueState) Equals(other *ValueState) bool {
	return slices.Equal(*v, *other)
}
