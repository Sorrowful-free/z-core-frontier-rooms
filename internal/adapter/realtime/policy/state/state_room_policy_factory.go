package state

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
)

type StateRoomPolicyFactory struct {
	logger logging.Logger
}

func NewStateRoomPolicyFactory(logger logging.Logger) *StateRoomPolicyFactory {
	return &StateRoomPolicyFactory{
		logger: logger,
	}
}

func (f *StateRoomPolicyFactory) CreateRoomPolicy() policy.RoomPolicy {
	return NewStateRoomPolicy(
		f.logger,
		codec.NewRoomStateCodec(&f.logger),
		codec.NewEntitiesStateCodec(),
		codec.NewInputStateCodec(),
		&codec.RpcStateCodec{},
		DefaultFullStateInterval,
		DefaultPatchStateInterval,
	)
}
