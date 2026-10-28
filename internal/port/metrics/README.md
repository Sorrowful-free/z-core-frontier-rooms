# port/metrics

Consumer-side port for process telemetry (P19). Implementation — [`adapter/metrics`](../../adapter/metrics/README.md) (prometheus), export — `GET /metrics` on a separate admin listener ([`delivery/admin`](../../delivery/admin/README.md)).

## Interface

`Metrics` — flat interface of typed signals (in the style of `port/logging.Logger`): components report domain events, mapping to metric names/labels lives inside the adapter. Cardinality is bounded by parameter types (reject counter by `domain.OpCode`).

| Group | Methods |
|-------|---------|
| Gauges | `SetActiveRooms`, `AddActivePeers`, `RoomIncomingDepth(n)`, `PeerOutboundDepth(n)`, `EnetHostPeers(hostID, n)`, `ReservationActiveSlots(delta)` |
| Rooms | `PeerJoined`, `PeerLeft`, `JoinRejected(op)`, `MasterElected`, `RoomTickObserved(kind, d)` |
| Queues | `RoomIncomingDropped`, `PeerOutboundDropped`, `EnetIncomingDropped`, `EnetHostLoopQueueLatency(d)` |
| Peer | `PeerSendError`, `PeerDisconnected`, `ReconnectSucceeded` |
| Reservation | `ReservationReserved`, `ReservationAdmitted`, `ReservationRevoked` |
| Sweeps | `OrphanAdmittedRevoked(n)`, `IdlePeersEvicted(n)`, `SweepRan(sweep)` |
| HTTP | `HTTPRequestObserved(route, method, status, d)` |

`PeerJoined`/`PeerLeft` count membership transitions on the room loop: `Replace` = leave + join (a pair of events, peers gauge without a delta). Membership metrics are tied to the actual mutation of the `peers` map (P19b): re-join of the same peer and repeated leave do not distort `active_peers`.

Queue-depth gauge signals are `len(chan)` samples at enqueue/dequeue points (a last-write-wins approximation across instances, not a strict sum). `JoinRejected` is counted only after the reject frame is sent successfully: `delivery/errors.SendJoinReject` returns the send error, and the caller emits based on its result.

## Noop

`Noop()` / `OrNoop(m)` — stub and nil normalization: components accept `nil` in constructors and normalize to `Noop`, so direct construction in tests is safe without a mock.
