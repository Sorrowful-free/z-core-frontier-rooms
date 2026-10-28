// Package peer — a parameterized test realtime.Peer: configurable
// id/nick/ping/alive/deliverErr and capture of delivered PeerEvent
// (snapshot/clear/filter by opcode). Consolidates hand-written capture stubs
// (stubPeer/capturePeer/flakyPeer/mortalPeer etc., see docs/plans/P16-test-hygiene.md).
//
// Deliver semantics mirror the production peer: an injected deliverErr is
// returned first, then a dead transport (SetAlive(false)/Stop) rejects frames
// with ErrPeerDead, and a live peer captures the frame (unless capture is off).
package peer

import (
	"errors"
	"sync"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain/events"
	portrealtime "github.com/Sorrowful-free/z-core-frontier-rooms/internal/port/realtime"
)

// ErrPeerDead is returned by Deliver of a dead peer (SetAlive(false) or after
// Stop) — models a transport failure, like a production peer after Stop.
var ErrPeerDead = errors.New("peer transport dead")

// Peer — a test realtime.Peer with fault injection and delivered-frame capture.
type Peer struct {
	id   domain.PeerID
	nick string

	mu         sync.Mutex
	ping       int64
	alive      bool
	capture    bool
	deliverErr error
	delivered  []events.PeerEvent
}

// Option overrides the default peer config (ping=0, alive=true, capture on).
type Option func(*Peer)

// WithPing sets the Ping() value — used in master election scenarios.
func WithPing(ping int64) Option {
	return func(p *Peer) {
		p.ping = ping
	}
}

// WithoutCapture disables delivered-frame capture — for no-op peers
// in benchmarks, where accumulating delivered would skew memory/timings.
func WithoutCapture() Option {
	return func(p *Peer) {
		p.capture = false
	}
}

// New creates a live peer with frame capture enabled.
func New(id domain.PeerID, nick string, opts ...Option) *Peer {
	p := &Peer{id: id, nick: nick, alive: true, capture: true}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *Peer) GetID() domain.PeerID { return p.id }
func (p *Peer) GetNickName() string  { return p.nick }

// Start — no-op: the fake runs no read/write loops, sink is ignored.
func (p *Peer) Start(portrealtime.EventSink) error { return nil }
func (p *Peer) Ping() int64                        { p.mu.Lock(); defer p.mu.Unlock(); return p.ping }
func (p *Peer) IsAlive() bool                      { p.mu.Lock(); defer p.mu.Unlock(); return p.alive }

// Stop marks the peer dead: further Deliver calls are rejected with ErrPeerDead —
// mirrors the production peer behavior ("peer stopped").
func (p *Peer) Stop() error {
	p.mu.Lock()
	p.alive = false
	p.mu.Unlock()
	return nil
}

// Deliver: injected deliverErr → dead transport (ErrPeerDead) → frame capture.
func (p *Peer) Deliver(ev events.PeerEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deliverErr != nil {
		return p.deliverErr
	}
	if !p.alive {
		return ErrPeerDead
	}
	if p.capture {
		p.delivered = append(p.delivered, ev)
	}
	return nil
}

// SetPing changes the Ping() value (bench scenarios of the ping-resync tick).
func (p *Peer) SetPing(ping int64) {
	p.mu.Lock()
	p.ping = ping
	p.mu.Unlock()
}

// SetAlive toggles transport liveness: !alive → IsAlive false and Deliver
// is rejected with ErrPeerDead (models "transport died without leave").
func (p *Peer) SetAlive(alive bool) {
	p.mu.Lock()
	p.alive = alive
	p.mu.Unlock()
}

// SetDeliverErr enables/clears a permanent Deliver error (nil — healthy peer).
func (p *Peer) SetDeliverErr(err error) {
	p.mu.Lock()
	p.deliverErr = err
	p.mu.Unlock()
}

// Snapshot — a copy of captured frames in delivery order.
func (p *Peer) Snapshot() []events.PeerEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]events.PeerEvent, len(p.delivered))
	copy(out, p.delivered)
	return out
}

// EventsByOpcode — captured frames with the given opcode.
func (p *Peer) EventsByOpcode(opcode domain.OpCode) []events.PeerEvent {
	return FilterByOpcode(p.Snapshot(), opcode)
}

// Clear resets captured frames (count events "from this moment on").
func (p *Peer) Clear() {
	p.mu.Lock()
	p.delivered = nil
	p.mu.Unlock()
}

// FilterByOpcode — events from a slice with the given opcode (for frames captured
// outside the peer — e.g., by a spy implementation of room.Send).
func FilterByOpcode(peerEvents []events.PeerEvent, opcode domain.OpCode) []events.PeerEvent {
	var out []events.PeerEvent
	for _, ev := range peerEvents {
		if ev.Frame.OpCode == opcode {
			out = append(out, ev)
		}
	}
	return out
}

// Opcodes — opcodes of the slice's events (for diagnostic messages).
func Opcodes(peerEvents []events.PeerEvent) []domain.OpCode {
	out := make([]domain.OpCode, len(peerEvents))
	for i, ev := range peerEvents {
		out[i] = ev.Frame.OpCode
	}
	return out
}

var _ portrealtime.Peer = (*Peer)(nil)
