package room

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

// PeerSession — session-handle admitted-соединения (P20): возвращается из
// admit-потоков (join/reconnect), handler несёт его до teardown.
// DisconnectPeer удаляет из комнаты именно этот peer-объект — identity-check
// в lifecycle гарантирует, что stale teardown старого conn после Replace
// не выкинет нового peer'а (ErrPeerStale).
type PeerSession struct {
	RoomID domain.RoomID
	PeerID domain.PeerID
	Peer   realtime.Peer
}
