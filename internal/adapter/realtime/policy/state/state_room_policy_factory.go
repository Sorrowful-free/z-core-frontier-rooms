package state

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
)

type StateRoomPolicyFactory struct {
	logger  logging.Logger
	metrics portmetrics.Metrics
}

func NewStateRoomPolicyFactory(logger logging.Logger, m portmetrics.Metrics) *StateRoomPolicyFactory {
	return &StateRoomPolicyFactory{
		logger:  logger,
		metrics: portmetrics.OrNoop(m),
	}
}

func (f *StateRoomPolicyFactory) CreateRoomPolicy() policy.RoomPolicy {
	p := NewStateRoomPolicy(
		f.logger,
		codec.NewRoomStateCodec(),
		codec.NewEntitiesStateCodec(),
		codec.NewInputStateCodec(),
		codec.NewRpcStateCodec(),
		DefaultFullStateInterval,
		DefaultPatchStateInterval,
	)
	// Счётчик master-elections живёт в машине эпизодов; вливается
	// полем, чтобы сигнатура NewStateRoomPolicy осталась стабильной.
	p.master.metrics = f.metrics
	return p
}
