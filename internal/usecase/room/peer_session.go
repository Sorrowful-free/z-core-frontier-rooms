package room

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

// PeerSession — session handle of an admitted connection (P20): returned
// by the admit flows (join/reconnect), the handler carries it until
// teardown. DisconnectPeer removes exactly this peer object from the room —
// the lifecycle identity check guarantees that a stale teardown of the old
// conn after Replace won't evict the new peer (ErrPeerStale).
type PeerSession struct {
	RoomID domain.RoomID
	PeerID domain.PeerID
	Peer   realtime.Peer
}
