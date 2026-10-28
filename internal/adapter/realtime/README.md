# adapter/realtime

Room runtime: `Room`, `Peer`, factories.

The factories (`NewRoomFactory`, `NewPeerFactory`) accept `port/metrics.Metrics` (nil-safe) and inject it as a field into the created `Room`/`Peer` — the `NewRoom`/`NewPeer` signatures remain stable for white-box tests (default `Noop`). Emits: joins/leaves + gauge `active_peers` on lifecycle transitions (**on actual mutation of the `peers` map** — a repeated join/leave of the same peer causes no drift, P19b) and `-len(peers)` in `Room.Stop` (P31: room kill paths — `closeRoom`, DELETE, empty-death, shutdown — kill peers bypassing `Leave`, and `registry.DeleteRoom` always goes through `room.Stop`, so a single line covers all death paths without double-counting peers that already left), drops of full `incoming`/`outbound`, gauge samples of both queue depths (`len(chan)` on enqueue/dequeue, an approximation), tick durations `rooms_room_tick_seconds{kind}`, send errors on a live connection `rooms_peer_send_errors_total` (expected teardown `!IsAlive` is not counted).

## Room

A single goroutine event loop (`processRoomEvents`):

- `incoming` — buffer `RoomIncomingQueueSize` (default 128); full → drop (`domain.ErrQueueFull`); frames → `OnMessage`
- `lifecycle` — Join / Leave / Replace (serialized with OnMessage)
- tickers — `OnTickFullState` / `OnTickPatchState` from `policy.TickIntervals()`

Public operations:

- `Join` — capacity check + insertion into the peers map + `OnJoin` run **atomically on the room loop** (P13): the peer is visible in `r.peers` before the policy inserts it into state — ticks (`pruneStalePeers`) cannot mistake the newly joined peer for stale. On `OnJoin` error the peers insertion is rolled back. Capacity pre-check on the caller — early answer, authoritative check in the loop. An occupied peerID is rejected with `ErrPeerAlreadyInRoom` (P20, double-resume race: a silent overwrite would orphan the losing peer object) — the resume flow forces the refusal through `Replace`.
- `Leave` — removal from the peers map + `OnLeave` atomically on the room loop: the peer is out of the registry **before** the policy runs; an `OnLeave` error is returned, and on rollback the state record is cleaned up by the next prune tick (`GetPeer` → `ErrPeerNotFound` → stale). Identity check (P20): the entry is removed from the map only if `peers[id]` is the same peer object, otherwise `domain.ErrPeerStale` (a stale teardown of the old conn after `Replace` does not evict the new peer). Double-teardown dedup (P23) is authoritative at the registry level: a repeated leave of an already-removed peer (both callers passed the pre-check before serialization on the loop) is rejected with `domain.ErrPeerNotFound` — the same sentinel as the pre-check — before `OnLeave` and metrics run, so a single broadcast does not depend on policy behavior.
- `Replace` — `lifecycleLeave(old)` → `lifecycleJoin(new)`; on join error — rollback via `lifecycleJoin(old)`
- `Start`/`Stop` — `sync.Once` guards; repeated calls are idempotent
- `Send` / `Deliver` — outbound `PeerEvent` / inbound `RoomEvent`

### `Deliver` contract

| Outcome | `Room.Deliver` / `Peer.Deliver` | Caller behavior |
|---------|---------------------------------|-----------------|
| Success | `nil` | — |
| Queue full | `domain.ErrQueueFull` | log; do **not** drop the connection (`processIncomingEvents` keeps reading) |
| Stopped | `room stopped` / `peer stopped` error | fatal for the peer loop (except an already-stopped one) |
| Other error | other `Deliver` error | `processIncomingEvents` logs, closes the `connection` and exits — accelerated cleanup of a half-dead peer |

`Room.Send`: `ErrQueueFull` on an individual peer — `Warn` and the broadcast continues to the rest (full-state/patch do not fail wholesale because of one full outbound). Any other `Deliver` error (incl. `peer stopped`) **aborts the broadcast** and is returned to the caller — the frame may not reach some peers.

## Shutdown

`Room.Stop()` terminates the event loop by cancelling the room context (`cancel`); `processRoomEvents` exits on `<-ctx.Done()`.

The `incoming` and `lifecycle` channels are **intentionally never closed**: external goroutines (peers, use cases) that are not part of `Room.wg` write to them. Closing them after `wg.Wait()` caused `send on closed channel` panics.

After cancel, producers (`Deliver`, `dispatchLifecycle`) return a "room stopped" error via `ctx.Err()`, without blocking or panicking.

The same contract applies to `peer.outbound`: the channel is never closed, the consumer exits on `ctx.Done()` — convention [ADR 0003](../../../docs/adr/0003-channel-ownership.md).

Tests: `tests/adapter/realtime/room_stop_test.go`, `tests/adapter/registry/room_registry_stop_test.go` (concurrent `Deliver`/`Join`/`Leave` + `Stop`).

## Peer

- `NickName` is set in `NewPeer` from the ticket (`JoinRoom` → `claims.NickName`)
- The peer does **not** store its room (P26): the entry point of the `incoming` mailbox is the narrow port `port/realtime.EventSink` (`Deliver(RoomEvent)`), passed as a parameter to `Start(sink)` and living only in the read goroutine's closure. The start-after-join contract (`Start` only after a successful `Join`/`Replace`) is a caller-side invariant, fixed in the port.
- The peer's lifetime ctx is a separate `NewPeer`/`CreatePeer` argument (the caller passes `room.Context()`); the room is not needed to construct a peer.
- I/O via `transport.Connection`
- `outbound` — buffer `PeerOutboundQueueSize` (default 128); full → `domain.ErrQueueFull` (the room loop is not stalled)
- `Ping()` for refresh in the policy patch tick
- `IsAlive()` — delegates to `transport.Connection`; `false` → the peer is subject to idle eviction ([ADR 0001](../../../docs/adr/0001-idle-peer-eviction.md))

### Peer Stop / drain (P30)

`Peer.Stop()` is not fire-and-forget: `cancel` → **drain** `outbound` → `conn.Close` → `wg.Wait`. On ctx cancellation the write loop enters the drain phase: it reads `outbound` to empty (`select`+`default`), calling `Send` for each leftover, and closes `writeDone`. `Stop` waits for `writeDone` no longer than `drainTimeout`; on deadline it goes for a force `conn.Close` (which also interrupts a blocked `Send` — the read loop must not hang until close, so waiting for the whole `wg` before `conn.Close` would deadlock). `drainTimeout = 0` — drain disabled.

Contract: frames whose `Deliver` committed **before** `Stop` began get a `Send` attempt before `Close`; a `Deliver` racing `Stop` (passed the ctx check but enqueued after the write loop observed an empty queue) is **unspecified**. The guarantee is "`Send` was called before `Close`", not "the client received it": after a normal close() the kernel flushes the buffer; on ENet `SendBytes` lands in the host loop's FIFO before `DisconnectLater` — the ordering is strict de jure. The value comes from `PeerFactory` (`TransportConfig.PeerCloseDrainTimeout`); `NewPeer` uses a package default of 1s (signature stable for white-box tests, same pattern as `metrics`).

## Policy

Contract: `port/realtime/policy.RoomPolicy`.  
Implementation: [policy/state/README.md](policy/state/README.md).

## Codec

Wire encode/decode: [codec/README.md](codec/README.md).

## In-room errors

`room_in_room_error.go` — on an `OnMessage` error, an OpCode frame to the sender (`domain.InRoomErrorOpCode`).
