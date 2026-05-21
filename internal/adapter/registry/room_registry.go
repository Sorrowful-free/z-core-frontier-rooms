package registry

import (
	"context"
	"fmt"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type RoomRegistry struct {
	roomFactory        realtime.RoomFactory
	roomHandlerFactory realtime.RoomHandlerFactory
	rooms              map[domain.RoomID]realtime.Room
	logger             logging.Logger
}

func NewRoomRegistry(roomFactory realtime.RoomFactory, roomHandlerFactory realtime.RoomHandlerFactory, logger logging.Logger) *RoomRegistry {
	return &RoomRegistry{
		roomFactory:        roomFactory,
		roomHandlerFactory: roomHandlerFactory,
		rooms:              make(map[domain.RoomID]realtime.Room),
		logger:             logger,
	}
}

func (r *RoomRegistry) CreateRoom(ctx context.Context, id domain.RoomID) (realtime.Room, error) {
	handler := r.roomHandlerFactory.CreateRoomHandler()
	room := r.roomFactory.CreateRoom(id, handler)
	r.rooms[id] = room
	return room, nil
}

func (r *RoomRegistry) GetRoom(ctx context.Context, id domain.RoomID) (realtime.Room, error) {
	room, ok := r.rooms[id]
	if !ok {
		return nil, fmt.Errorf("room not found: %s", id)
	}
	return room, nil
}

func (r *RoomRegistry) DeleteRoom(ctx context.Context, id domain.RoomID) error {
	delete(r.rooms, id)
	return nil
}

func (r *RoomRegistry) GetList(ctx context.Context) ([]realtime.Room, error) {
	rooms := make([]realtime.Room, 0, len(r.rooms))
	for _, room := range r.rooms {
		rooms = append(rooms, room)
	}
	return rooms, nil
}
