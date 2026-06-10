package admission

import (
	"context"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type Admission interface {
	TTL() time.Duration
	Issue(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, nickName string, password string) ([]byte, error)
	Validate(ctx context.Context, token []byte) (domain.Claims, error)
}
