package realtime

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"

type RoomFactory interface {
	CreateRoom(id domain.RoomID, handler RoomHandler) Room
}
