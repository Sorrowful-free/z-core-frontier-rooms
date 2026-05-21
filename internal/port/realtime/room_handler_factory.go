package realtime

type RoomHandlerFactory interface {
	CreateRoomHandler() RoomHandler
}
