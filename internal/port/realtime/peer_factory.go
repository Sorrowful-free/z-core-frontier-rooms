package realtime

import (
	"context"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/logging"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/transport"
)

type PeerFactory interface {
	// CreatePeer конструирует peer'а поверх connection; ctx — жизненный
	// цикл peer'а (caller передаёт room.Context()), комната конструкции
	// не нужна — входная точка mailbox'а приходит в Peer.Start (P26).
	CreatePeer(ctx context.Context, id domain.PeerID, nickName string, connection transport.Connection, logger logging.Logger) (Peer, error)
}
