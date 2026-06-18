package bench

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/state"
)

// BuildRoomState returns a room with peerCount peers (IDs 1..N) for benchmarks.
func BuildRoomState(peerCount int) *state.RoomState {
	room := state.NewRoomState(domain.RoomID(1), 16, "")
	for i := 1; i <= peerCount; i++ {
		room.Peers[domain.PeerID(i)] = state.PeerState{
			NickName: "peer",
			Ping:     int64(i),
		}
	}
	return room
}
