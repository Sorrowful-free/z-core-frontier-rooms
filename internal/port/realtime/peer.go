package realtime

import (
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
)

type Peer interface {
	GetID() domain.PeerID
	GetNickName() string
	Ping() int64
	// IsAlive — false: the peer's transport is dead, the peer is subject to idle eviction.
	IsAlive() bool

	// Start launches the peer's read/write goroutines; sink is the mailbox
	// of the room that has already accepted the peer. Ordering contract
	// (Start-after-join): Start is called only after a successful
	// Join/Replace — sink is not "some room" but the entry point of the
	// owning room. The peer cannot verify membership itself (it knows
	// nothing about rooms — intentionally, P26): the invariant is
	// caller-side. The sink lives only in the read goroutine's closure and
	// dies with it — the peer holds no reference to the room.
	Start(sink EventSink) error

	// Stop — real termination of both goroutines, not fire-and-forget:
	// cancel → drain outbound (the write loop flushes committed frames,
	// bounded by the drain timeout) → conn.Close → the read loop exits on
	// a Receive error, wg.Wait waits for both. Contract: frames whose
	// Deliver committed before Stop began get a Send attempt before Close;
	// a Deliver racing with Stop (passed the ctx check, enqueued after the
	// write loop observed an empty queue) is unspecified — the frame may
	// be lost. Ordering: Stop after the peer has been removed from the
	// room (Leave/Replace/rollback) — a stopped peer in the room registry
	// is a zombie until the idle sweep. Fallback: the conn died on its own
	// (network) → the read loop exits on a Receive error without Stop; the
	// write loop waits on ctx.Done until Stop from the evict path. Stop is
	// expected after Start: before Start the write loop is not running —
	// the drain wait is skipped and the connection is simply closed.
	Stop() error

	Deliver(peerEvent events.PeerEvent) error
}
