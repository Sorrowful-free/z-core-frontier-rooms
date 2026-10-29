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
		f.cfg.FullStateInterval,
		f.cfg.PatchStateInterval,
	)
	// The master-elections counter lives in the episode machine; it is
	// injected via the field so the NewStateRoomPolicy signature stays
	// stable.
	p.master.metrics = f.metrics
	// The master-migration mode (P33) — the same field injection from the deploy config.
	p.allowMasterMigration = f.cfg.AllowMasterMigration
	return p
}
