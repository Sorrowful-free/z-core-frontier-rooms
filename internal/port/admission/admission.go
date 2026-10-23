package admission

import (
	"context"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type Admission interface {
	TTL() time.Duration
	// Issue подписывает ticket; hostID - strict binding к ENet host'у
	// (domain.HostIDAny - ticket без привязки, admit на любом host'е).
	Issue(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, nickName string, hostID domain.HostID) ([]byte, error)
	Validate(ctx context.Context, token []byte) (domain.Claims, error)
}
