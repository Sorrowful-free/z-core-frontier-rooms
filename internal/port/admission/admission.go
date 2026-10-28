package admission

import (
	"context"
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

type Admission interface {
	TTL() time.Duration
	// Issue signs the ticket; hostID is a strict binding to the ENet host
	// (domain.HostIDUnbound - ticket without a binding: rejected on ENet-admit,
	// valid only for WS).
	Issue(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, nickName string, hostID domain.HostID) ([]byte, error)
	// IssueReconnect signs a reconnect token (P20): credential resume
	// under the same peerID; hostID is the same strict binding as in Issue.
	IssueReconnect(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID, hostID domain.HostID) ([]byte, error)
	Validate(ctx context.Context, token []byte) (domain.Claims, error)
}
