package state

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/adapter/realtime/codec"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
)

type StateRoomPolicyFactory struct {
	logger  logging.Logger
	cfg     StatePolicyConfig
	metrics portmetrics.Metrics
}

func NewStateRoomPolicyFactory(logger logging.Logger, cfg StatePolicyConfig, m portmetrics.Metrics) *StateRoomPolicyFactory {
	return &StateRoomPolicyFactory{
		logger:  logger,
		cfg:     cfg,
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
	// Режим master-миграции (P33) — тот же полевой влейв из конфига деплоя.
	p.allowMasterMigration = f.cfg.AllowMasterMigration
	return p
}
