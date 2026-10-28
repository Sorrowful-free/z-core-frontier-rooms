package realtime

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
)

type RoomFactory struct {
	logger        logging.Logger
	policyFactory policy.RoomPolicyFactory
	incomingQueue int
	metrics       portmetrics.Metrics
}

func NewRoomFactory(logger logging.Logger, policyFactory policy.RoomPolicyFactory, incomingQueue int, m portmetrics.Metrics) *RoomFactory {
	return &RoomFactory{
		logger:        logger,
		policyFactory: policyFactory,
		incomingQueue: incomingQueue,
		metrics:       portmetrics.OrNoop(m),
	}
}

func (f *RoomFactory) CreateRoom(ctx context.Context, id domain.RoomID, capacity int, attributes domain.RoomAttributes) (realtime.Room, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// The concrete policy is bound to the factory at wiring time; the
	// master-migration mode is a global deploy config (P33), not a
	// create parameter.
	policy := f.policyFactory.CreateRoomPolicy()
	room := NewRoom(ctx, id, policy, capacity, attributes, f.incomingQueue, f.logger)
	// Metrics are injected by the factory via the field: the NewRoom
	// signature stays stable for white-box tests (the Noop default is in
	// the constructor).
	room.metrics = f.metrics
	return room, nil
}
