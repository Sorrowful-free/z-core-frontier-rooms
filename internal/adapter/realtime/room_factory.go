package realtime

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime/policy"
)

type RoomFactory struct {
	logger        logging.Logger
	policyFactory policy.RoomPolicyFactory
	incomingQueue int
}

func NewRoomFactory(logger logging.Logger, policyFactory policy.RoomPolicyFactory, incomingQueue int) *RoomFactory {
	return &RoomFactory{
		logger:        logger,
		policyFactory: policyFactory,
		incomingQueue: incomingQueue,
	}
}

func (f *RoomFactory) CreateRoom(ctx context.Context, id domain.RoomID, capacity int, attributes domain.RoomAttributes) (realtime.Room, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	policy := f.policyFactory.CreateRoomPolicy()
	return NewRoom(ctx, id, policy, capacity, attributes, f.incomingQueue, f.logger), nil
}
