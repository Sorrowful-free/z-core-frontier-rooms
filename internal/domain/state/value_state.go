package state

import (
	"bytes"
	"slices"
)

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

// Clone returns a deep copy of opaque value bytes.
func (v ValueState) Clone() ValueState {
	return bytes.Clone(v)
}
