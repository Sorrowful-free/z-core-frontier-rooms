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

func (f *RoomFactory) CreateRoom(ctx context.Context, id domain.RoomID, capacity int, attributes domain.RoomAttributes, policyKind domain.RoomPolicyKind) (realtime.Room, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// policyKind на этом уровне не нужен: конкретная policy привязана к
	// фабрике при сборке — dispatch по kind делает KindRoomFactory (P29).
	policy := f.policyFactory.CreateRoomPolicy()
	room := NewRoom(ctx, id, policy, capacity, attributes, f.incomingQueue, f.logger)
	// Метрики вливаются фабрикой полем: сигнатура NewRoom держится
	// стабильной для white-box тестов (умолчание Noop - в конструкторе).
	room.metrics = f.metrics
	return room, nil
}
