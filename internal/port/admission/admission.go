package admission

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type Admission interface {
	Issue(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, password string) ([]byte, error)
	Validate(ctx context.Context, token []byte) (domain.Claims, error)
}
