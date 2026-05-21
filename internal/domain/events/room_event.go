package events

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"

type RoomEvent struct {
	PeerID domain.PeerID
	Frame  domain.Frame
}
