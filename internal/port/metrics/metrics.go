// Package metrics — consumer-side port for process telemetry (P19).
//
// A flat interface of typed signals (like `port/logging.Logger`):
// components report events in domain verbs, and the adapter
// (`internal/adapter/metrics`) maps them to prometheus metrics. Label
// cardinality is bounded by the parameter types (wire OpCode).
package metrics

import (
	"time"

	"github.com/Sorrowful-free/z-core-frontier-rooms/internal/domain"
)

// Metrics — port for exporting observability signals. All methods are
// error-free and non-blocking by contract (the implementation itself is
// responsible for thread-safety; prometheus collectors are concurrency-safe).
type Metrics interface {
	// SetActiveRooms — gauge: current number of live rooms in the registry.
	SetActiveRooms(rooms int)
	// AddActivePeers — gauge: delta of live peers summed across rooms.
	AddActivePeers(delta int)

	// PeerJoined — counter: peer entered a room (lifecycle room loop).
	PeerJoined()
	// PeerLeft — counter: peer removed from a room (lifecycle room loop).
	PeerLeft()
	// PeerDisconnected — counter: peer entered reconnect grace
	// (reservation.MarkDisconnected, P20).
	PeerDisconnected()
	// ReconnectSucceeded — counter: resume under the same peerID completed
	// successfully (ReconnectRoomUseCase, P20).
	ReconnectSucceeded()

	// JoinRejected — counter: join/admit reject frame on the wire, label —
	// the rejection OpCode (bounded: finite set of reject opcodes).
	JoinRejected(op domain.OpCode)

	// RoomIncomingDropped — counter: frame dropped from the full
	// room.incoming queue (ErrQueueFull).
	RoomIncomingDropped()
	// PeerOutboundDropped — counter: frame dropped from the full
	// peer.outbound queue (ErrQueueFull).
	PeerOutboundDropped()
	// EnetIncomingDropped — counter: frame dropped from the full
	// ENet session queue (EnetIncomingQueueSize).
	EnetIncomingDropped()

	// RoomIncomingDepth — gauge: depth of the room.incoming queue.
	// A `len(chan)` sample at enqueue/dequeue points — an approximation
	// (last-write-wins between rooms), not an exact sum.
	RoomIncomingDepth(n int)
	// PeerOutboundDepth — gauge: depth of the peer.outbound queue.
	// A `len(chan)` sample at enqueue/dequeue points — an approximation.
	PeerOutboundDepth(n int)
	// RoomTickObserved — room loop tick duration (summary);
	// kind — "full" | "patch" (bounded, constants in adapter/realtime).
	RoomTickObserved(kind string, d time.Duration)
	// PeerSendError — counter: Send error on a live connection
	// (expected teardown with !IsAlive is not counted).
	PeerSendError()

	// EnetHostPeers — gauge: live ENet sessions on the host; the hostID
	// label is bounded by the number of ENET_LISTEN_PORTS ports.
	EnetHostPeers(hostID domain.HostID, peers int)
	// EnetHostLoopQueueLatency — duration of a command's wait in the
	// HostLoop queue (enqueue → exec, summary; aggregated per host).
	EnetHostLoopQueueLatency(d time.Duration)

	// MasterElected — counter: room got a new master
	// (initial election and re-elections, master_machine).
	MasterElected()

	// ReservationReserved — counter: reservation slot taken (Reserve).
	ReservationReserved()
	// ReservationAdmitted — counter: slot transitioned to admitted (Admit).
	ReservationAdmitted()
	// ReservationRevoked — counter: slot freed (any real delete:
	// Revoke, expired in Admit, lazy sweep of expired ones).
	ReservationRevoked()
	// ReservationActiveSlots — gauge: delta of live reservation slots
	// (reserve +1; each real slot deletion -1; room tombstone on
	// UnregisterRoom -len(peers)).
	ReservationActiveSlots(delta int)

	// OrphanAdmittedRevoked — counter: orphan admitted slots revoked
	// per SweepOrphanAdmittedUseCase pass.
	OrphanAdmittedRevoked(n int)
	// IdlePeersEvicted — counter: idle peers evicted per
	// EvictIdlePeersUseCase pass.
	IdlePeersEvicted(n int)
	// SweepRan — counter: background sweep pass (separate from
	// processed); sweep — "orphan_admitted" | "idle_peers".
	SweepRan(sweep string)

	// HTTPRequestObserved — public API HTTP request duration
	// (histogram); route — Fiber route template (not raw path,
	// cardinality bounded by the router), status — HTTP code.
	HTTPRequestObserved(route, method string, status int, d time.Duration)
}

// noopMetrics — stub implementation: all signals are discarded.
type noopMetrics struct{}

func (noopMetrics) SetActiveRooms(int)                                     {}
func (noopMetrics) AddActivePeers(int)                                     {}
func (noopMetrics) PeerJoined()                                            {}
func (noopMetrics) PeerLeft()                                              {}
func (noopMetrics) PeerDisconnected()                                      {}
func (noopMetrics) ReconnectSucceeded()                                    {}
func (noopMetrics) JoinRejected(domain.OpCode)                             {}
func (noopMetrics) RoomIncomingDropped()                                   {}
func (noopMetrics) PeerOutboundDropped()                                   {}
func (noopMetrics) EnetIncomingDropped()                                   {}
func (noopMetrics) RoomIncomingDepth(int)                                  {}
func (noopMetrics) PeerOutboundDepth(int)                                  {}
func (noopMetrics) RoomTickObserved(string, time.Duration)                 {}
func (noopMetrics) PeerSendError()                                         {}
func (noopMetrics) EnetHostPeers(domain.HostID, int)                       {}
func (noopMetrics) EnetHostLoopQueueLatency(time.Duration)                 {}
func (noopMetrics) MasterElected()                                         {}
func (noopMetrics) ReservationReserved()                                   {}
func (noopMetrics) ReservationAdmitted()                                   {}
func (noopMetrics) ReservationRevoked()                                    {}
func (noopMetrics) ReservationActiveSlots(int)                             {}
func (noopMetrics) OrphanAdmittedRevoked(int)                              {}
func (noopMetrics) IdlePeersEvicted(int)                                   {}
func (noopMetrics) SweepRan(string)                                        {}
func (noopMetrics) HTTPRequestObserved(string, string, int, time.Duration) {}

// Noop — Metrics stub: accepts and discards all signals.
// Default for components built without telemetry (tests).
func Noop() Metrics {
	return noopMetrics{}
}

// OrNoop normalizes nil to Noop — DI nil-safety point:
// a constructor may accept nil instead of an implementation.
func OrNoop(m Metrics) Metrics {
	if m == nil {
		return noopMetrics{}
	}
	return m
}
