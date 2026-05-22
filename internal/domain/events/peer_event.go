package events

import "github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"

type PeerEvent struct {
	// PeerID — целевой peer; PeerIDInvalid — broadcast по комнате.
	PeerID domain.PeerID
	// ExcludePeerID — при broadcast не доставлять этому peer (обычно отправитель).
	ExcludePeerID domain.PeerID
	Frame         domain.Frame
}
