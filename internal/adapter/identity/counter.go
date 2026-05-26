package identity

import (
	"context"
	"sync/atomic"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	portidentity "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/identity"
)

var _ portidentity.Allocator = (*Counter)(nil)

type Counter struct {
	roomID uint32
	peerID uint32
}

func NewCounter() *Counter {
	return NewCounterAt(0, 0)
}

// NewCounterAt continues numbering from roomID and peerID (next Allocate* uses nextID on each field).
// Tests use it to simulate uint32 wrap, e.g. roomID = math.MaxUint32.
func NewCounterAt(roomID, peerID uint32) *Counter {
	return &Counter{
		roomID: roomID,
		peerID: peerID,
	}
}

func (c *Counter) AllocateRoomID(ctx context.Context) (domain.RoomID, error) {
	if err := ctx.Err(); err != nil {
		return domain.RoomIDInvalid, err
	}
	return domain.RoomID(nextID(&c.roomID)), nil
}

func (c *Counter) AllocatePeerID(ctx context.Context) (domain.PeerID, error) {
	if err := ctx.Err(); err != nil {
		return domain.PeerIDInvalid, err
	}
	return domain.PeerID(nextID(&c.peerID)), nil
}

func nextID(counter *uint32) uint32 {
	id := atomic.AddUint32(counter, 1)
	if id != 0 {
		return id
	}
	return atomic.AddUint32(counter, 1)
}
