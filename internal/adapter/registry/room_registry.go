package registry

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	portmetrics "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/metrics"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type RoomRegistry struct {
	roomFactory realtime.RoomFactory
	mutex       sync.RWMutex
	rooms       map[domain.RoomID]realtime.Room
	logger      logging.Logger
	metrics     portmetrics.Metrics

	lifecycle       context.Context
	lifecycleCancel context.CancelFunc
}

func NewRoomRegistry(lifecycle context.Context, roomFactory realtime.RoomFactory, logger logging.Logger, m portmetrics.Metrics) *RoomRegistry {
	lifecycle, lifecycleCancel := context.WithCancel(lifecycle)
	return &RoomRegistry{
		roomFactory:     roomFactory,
		rooms:           make(map[domain.RoomID]realtime.Room),
		logger:          logger,
		metrics:         portmetrics.OrNoop(m),
		lifecycle:       lifecycle,
		lifecycleCancel: lifecycleCancel,
	}
}

func (r *RoomRegistry) CreateRoom(ctx context.Context, id domain.RoomID, capacity int, attributes domain.RoomAttributes, policyKind domain.RoomPolicyKind) (realtime.Room, error) {

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mutex.RLock()
	_, ok := r.rooms[id]
	r.mutex.RUnlock()
	if ok {
		return nil, fmt.Errorf("%w: %d", domain.ErrRoomAlreadyExists, id)
	}

	room, err := r.roomFactory.CreateRoom(r.lifecycle, id, capacity, attributes, policyKind)
	if err != nil {
		return nil, err
	}

	r.logger.Info("room created", "id", id)
	if err := room.Start(); err != nil {
		r.logger.Error("error starting room", "error", err)
		_ = room.Stop()
		return nil, err
	}

	r.mutex.Lock()
	if _, ok := r.rooms[id]; ok {
		r.mutex.Unlock()
		if err := room.Stop(); err != nil {
			r.logger.Error("error stopping racing room", "error", err, "id", id)
		}
		return nil, fmt.Errorf("%w: %d", domain.ErrRoomAlreadyExists, id)
	}
	r.rooms[id] = room
	// Gauge пересчитывается по map, а не инкрементами - самолечится
	// от любых рассинхронов счётчиков.
	r.metrics.SetActiveRooms(len(r.rooms))
	r.mutex.Unlock()

	return room, nil
}

func (r *RoomRegistry) GetRoom(ctx context.Context, id domain.RoomID) (realtime.Room, error) {

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mutex.RLock()
	defer r.mutex.RUnlock()
	room, ok := r.rooms[id]
	if !ok {
		return nil, fmt.Errorf("%w: %d", domain.ErrRoomNotFound, id)
	}
	return room, nil
}

func (r *RoomRegistry) DeleteRoom(ctx context.Context, id domain.RoomID) error {

	if err := ctx.Err(); err != nil {
		return err
	}

	r.mutex.Lock()
	room, ok := r.rooms[id]
	if !ok {
		r.mutex.Unlock()
		return fmt.Errorf("%w: %d", domain.ErrRoomNotFound, id)
	}
	delete(r.rooms, id)
	r.metrics.SetActiveRooms(len(r.rooms))
	r.mutex.Unlock()

	if err := room.Stop(); err != nil {
		r.logger.Error("error stopping room", "error", err, "id", id)
	}
	r.logger.Info("room deleted", "id", id)
	return nil
}

func (r *RoomRegistry) GetList(ctx context.Context) ([]realtime.Room, error) {

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mutex.RLock()
	defer r.mutex.RUnlock()
	rooms := make([]realtime.Room, 0, len(r.rooms))
	for _, room := range r.rooms {
		rooms = append(rooms, room)
	}
	return rooms, nil
}

func (r *RoomRegistry) Shutdown(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mutex.RLock()
	ids := make([]domain.RoomID, 0, len(r.rooms))
	for id := range r.rooms {
		ids = append(ids, id)
	}
	r.mutex.RUnlock()

	var stopErr error
	for _, id := range ids {
		if err := r.DeleteRoom(ctx, id); err != nil {
			stopErr = errors.Join(stopErr, err)
		}
	}
	r.lifecycleCancel()
	return stopErr
}
