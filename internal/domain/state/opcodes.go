package state

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"

const (
	OpCodeNone domain.OpCode = 0

	OpCodeFullState  domain.OpCode = 0x01
	OpCodePatchState domain.OpCode = 0x02

	OpCodeFullEntities  domain.OpCode = 0x03
	OpCodePatchEntities domain.OpCode = 0x04

	OpCodeFullInput  domain.OpCode = 0x05
	OpCodePatchInput domain.OpCode = 0x06

	OpCodeRpc domain.OpCode = 0x07
)
