package registry

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type RoomRegistry interface {
	GetRoom(ctx context.Context, id domain.RoomID) (realtime.Room, error)
	CreateRoom(ctx context.Context, id domain.RoomID, capacity int) (realtime.Room, error)
	DeleteRoom(ctx context.Context, id domain.RoomID) error
	GetList(ctx context.Context) ([]realtime.Room, error)
	// Shutdown останавливает все комнаты и отменяет lifecycle-контекст реестра.
	Shutdown(ctx context.Context) error
}
