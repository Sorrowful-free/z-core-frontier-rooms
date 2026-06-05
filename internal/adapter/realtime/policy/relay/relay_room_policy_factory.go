package relay

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
)

type RelayRoomPolicyFactory struct {
	logger logging.Logger
}

func NewRelayRoomPolicyFactory(logger logging.Logger) *RelayRoomPolicyFactory {
	return &RelayRoomPolicyFactory{
		logger: logger,
	}
}

func (f *RelayRoomPolicyFactory) CreateRoomPolicy() policy.RoomPolicy {
	return NewRelayRoomPolicy(f.logger)
}
