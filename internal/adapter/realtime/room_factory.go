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
}

func NewRoomFactory(logger logging.Logger, policyFactory policy.RoomPolicyFactory) *RoomFactory {
	return &RoomFactory{
		logger:        logger,
		policyFactory: policyFactory,
	}
}

func (f *RoomFactory) CreateRoom(ctx context.Context, id domain.RoomID) (realtime.Room, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	policy := f.policyFactory.CreateRoomPolicy()
	return NewRoom(ctx, id, policy, f.logger), nil
}
