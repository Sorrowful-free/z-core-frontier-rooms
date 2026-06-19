package room

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type RoomSummary struct {
	ID         domain.RoomID
	Attributes domain.RoomAttributes
	Peers      []PeerSummary
}

var EmptyRoomSummary = RoomSummary{
	ID:         domain.RoomIDInvalid,
	Attributes: nil,
	Peers:      []PeerSummary{},
}

func NewRoomSummary(id domain.RoomID, attributes domain.RoomAttributes, peers []PeerSummary) *RoomSummary {
	return &RoomSummary{
		ID:         id,
		Attributes: domain.CloneRoomAttributes(attributes),
		Peers:      peers,
	}
}

func NewRoomSummaryFromRoom(room realtime.Room) *RoomSummary {
	peers := room.GetPeers()
	peerSummaries := make([]PeerSummary, len(peers))
	for i, p := range peers {
		peerSummaries[i] = *NewPeerSummaryFromPeer(p)
	}
	return NewRoomSummary(room.GetID(), room.GetAttributes(), peerSummaries)
}

func (r *RoomSummary) IsValid() bool {
	return r.ID.IsValid() && len(r.Peers) > 0
}
