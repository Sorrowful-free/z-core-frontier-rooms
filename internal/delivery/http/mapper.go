package http

import (
	"encoding/base64"

	useroom "github.com/Sorrowful-free/z-core-frontier-rooms/internal/usecase/room"
)

func roomToResponse(summary useroom.RoomSummary) roomResponse {
	peers := make([]peerResponse, len(summary.Peers))
	for i, p := range summary.Peers {
		peers[i] = peerResponse{
			PeerID:   int64(p.PeerID),
			NickName: p.NickName,
			Ping:     p.Ping,
		}
	}
	return roomResponse{
		ID:    int64(summary.ID),
		Peers: peers,
	}
}

func roomsToResponse(summaries []useroom.RoomSummary) roomListResponse {
	rooms := make([]roomResponse, len(summaries))
	for i, s := range summaries {
		rooms[i] = roomToResponse(s)
	}
	return roomListResponse{Rooms: rooms}
}

func tokenToResponse(token []byte) issueTicketResponse {
	return issueTicketResponse{
		Token: base64.RawURLEncoding.EncodeToString(token),
	}
}
