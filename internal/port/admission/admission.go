package admission

import (
	"context"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type Admission interface {
	TTL() time.Duration
	// Issue подписывает ticket; hostID - strict binding к ENet host'у
	// (domain.HostIDUnbound - ticket без привязки: на ENet-admit отвергается,
	// живёт только для WS).
	Issue(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, nickName string, hostID domain.HostID) ([]byte, error)
	// IssueReconnect подписывает reconnect-токен (P20): credential resume
	// под тем же peerID; hostID - та же strict binding привязка, что у Issue.
	IssueReconnect(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, hostID domain.HostID) ([]byte, error)
	Validate(ctx context.Context, token []byte) (domain.Claims, error)
}
