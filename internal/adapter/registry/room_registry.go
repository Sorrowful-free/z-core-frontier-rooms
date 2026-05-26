package registry

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type RoomRegistry struct {
	roomFactory realtime.RoomFactory
	mutex       sync.RWMutex
	rooms       map[domain.RoomID]realtime.Room
	logger      logging.Logger

	lifecycle       context.Context
	lifecycleCancel context.CancelFunc
}

func NewRoomRegistry(lifecycle context.Context, roomFactory realtime.RoomFactory, logger logging.Logger) *RoomRegistry {
	lifecycle, lifecycleCancel := context.WithCancel(lifecycle)
	return &RoomRegistry{
		roomFactory:     roomFactory,
		rooms:           make(map[domain.RoomID]realtime.Room),
		logger:          logger,
		lifecycle:       lifecycle,
		lifecycleCancel: lifecycleCancel,
	}
}

func (r *RoomRegistry) CreateRoom(ctx context.Context, id domain.RoomID) (realtime.Room, error) {

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mutex.RLock()
	_, ok := r.rooms[id]
	r.mutex.RUnlock()
	if ok {
		return nil, fmt.Errorf("%w: %d", domain.ErrRoomAlreadyExists, id)
	}

	room, err := r.roomFactory.CreateRoom(r.lifecycle, id)
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
	r.rooms[id] = room
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
	defer r.mutex.Unlock()
	room, ok := r.rooms[id]
	if !ok {
		return fmt.Errorf("%w: %d", domain.ErrRoomNotFound, id)
	}
	if err := room.Stop(); err != nil {
		r.logger.Error("error stopping room", "error", err)
		return err
	}
	r.logger.Info("room deleted", "id", id)
	delete(r.rooms, id)
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
