package events

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"

type PeerEvent struct {
	// PeerID — target peer; PeerIDInvalid — broadcast to the room.
	PeerID domain.PeerID
	// ExcludePeerID — on broadcast, do not deliver to this peer (usually the sender).
	ExcludePeerID domain.PeerID
	Frame         domain.Frame
}
