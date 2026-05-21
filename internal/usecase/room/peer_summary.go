package room

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

type PeerSummary struct {
	PeerID   domain.PeerID
	NickName string
	Ping     int64
}

var EmptyPeerSummary = PeerSummary{
	PeerID:   domain.PeerIDInvalid,
	NickName: "",
	Ping:     -1,
}

func NewPeerSummary(peerID domain.PeerID, nickName string, ping int64) *PeerSummary {
	return &PeerSummary{
		PeerID:   peerID,
		NickName: nickName,
		Ping:     ping,
	}
}

func NewPeerSummaryFromPeer(peer realtime.Peer) *PeerSummary {
	return NewPeerSummary(peer.GetID(), peer.GetNickName(), peer.GetPing())
}

func (p *PeerSummary) IsValid() bool {
	return p.PeerID.IsValid() && p.Ping >= 0
}
