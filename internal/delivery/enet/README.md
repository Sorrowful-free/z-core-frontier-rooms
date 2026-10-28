# delivery/enet

UDP host (separate port from Fiber). Build tag `enet` + CGO.

**Client spec (Godot, primary transport):** [docs/client-protocol/enet.md](../../../docs/client-protocol/enet.md).

## Defaults (`DefaultEnetConfig`)

The config type is `transport/enet.EnetConfig` (`internal/adapter/transport/enet/enet_config.go`, no build tags); the handler takes it in `NewRoomsHandler`.

| Parameter | Value | Env |
|-----------|-------|-----|
| Ports | `[7777]` (CSV - N hosts/shards) | `ENET_LISTEN_PORTS` |
| Peer limit | `64` **per host** | `ENET_PEER_LIMIT` |
| Channel limit | `2` | — |
| Service timeout | `10ms` | — |
| Admit deadline (EventConnect → ticket) | `10s` | `ENET_ADMIT_TIMEOUT` |
| First packet | raw ticket (`admit`), max `TRANSPORT_MAX_INCOMING_FRAME_BYTES` | — |
| After admit | channel `0`, `[OpCode u8][payload]`; server send reliable | — |
| Inbound delivery | `PacketFlagReliable` → `DeliveryReliable`; without flag → `DeliveryUnreliable` (`flags_cgo.go`) | — |
| Oversized packet/frame | graceful disconnect (+ `DisconnectPeer` if already admitted) | — |
| Admit error | OpCode `0x40`–`0x5F` and `0x70`–`0x7F` (1 byte) → **delivered**, then disconnect | — |
| Admit timeout | peer did not send ticket within `AdmitTimeout` → disconnect without a wire frame | — |

`EnetConfig.Validate()` - ports (1–65535, no duplicates, ≥1)/limits/queues/`AdmitTimeout`; env `ENET_*` - `adapter/config.LoadFromEnv()` → `cfg.Enet` (`MaxIncomingFrameBytes`/`EnetIncomingQueueSize` are copied by cmd/rooms from `cfg.Transport`).

## Host sharding (P18)

Each port in `cfg.ListenPorts` gets its own `enetHost` (`id` = `domain.HostID` = index+1) with its own `HostLoop` and run goroutine. A room is transport-agnostic: its peers may sit on different hosts.

- **Balancing:** `RoomsHandler` implements `port/transport/enet.HostRegistry` - `AssignHost` picks the least-loaded host by live peers (`enetHost.peers` is incremented on `EventConnect`, decremented in `sess.close`), round-robin tie-break. `IssueTicketUseCase` puts `HostID` into claims and `enet_port` into the HTTP response.
- **Strict binding:** the admit facade (`AdmitPeerUseCase`) compares `claims.HostID` with `host.id` before consuming a slot - on a foreign host join-reject `ErrWrongHost` → `OpWrongHost` (`0x77`) + disconnect; `HostIDUnbound` (a ticket without binding — issued without `HostRegistry`, WS-only) is rejected on ENet admit with the same `OpWrongHost`. A reconnect token (kind `2`) inherits the original ticket's binding (P20).
- **Peer→host binding:** each host has its own `EnetConnectionFactory` (`WithLoop` bound to its `HostLoop` - `EnetConnection.Ping`/peer operations are dispatched to the correct executor).
- **Fail-fast:** the first failed run (bind failure) cancels the mux's shared ctx; `Err()` = the first error.

## Listener lifecycle

`RoomsHandler.Listen() *ListenHandle`: `Done()` - exit of **all** host loops, `Err()` - terminal error (bind failure), `Stop()` - ctx cancel + waiting for each loop's real exit. `main.go` fails the process on a bind error.

Graceful shutdown: on ctx/Stop the host loop closes all sessions (`conn.Close` → `peer.DisconnectLater` → `closed` chan wakes peers' Receive goroutines), drains via service iterations, and calls `host.Destroy()`.

## Metrics (P19/P19b)

`NewRoomsHandler` takes `port/metrics.Metrics` (nil-safe): admit reject frames are counted in `rooms_join_rejects_total{opcode}` only on a successful send (including `OpWrongHost` `0x77`; a send error is Warn-logged), a drop from a full ENet session queue — `rooms_queue_drops_total{queue="enet_incoming"}`, live sessions per host — `rooms_enet_host_peers{host}` (the same number as the least-loaded counter), command wait time in the `HostLoop` queue (enqueue → exec) — `rooms_enet_host_loop_queue_latency_seconds` (aggregated across hosts). `NewHostLoop` takes `Metrics` as its last parameter (nil-safe).

Disconnect is graceful everywhere (`DisconnectLater` in `EnetConnection.Close`): queued reliable packets — including join reject — go out before the teardown. `DisconnectNow` is not used. The same mechanism guarantees delivery of `OpRoomClosed` (`0x82`) on room death in a no-migration deployment (P33): the frame and the disconnect ride the same host loop FIFO.

## libenet serialization

The libenet C API is not thread-safe: the host's entire C API is serialized by its `HostLoop` (`adapter/transport/enet`, P15). `host.Service` runs as a blocking `Call` command into the loop; peer/packet dispatch operations (`session_cgo`/`frame_cgo`) — `Call`/`Do` into the same loop. Dispatch itself executes on the run goroutine, not the loop goroutine: the admit/teardown path (`AdmitPeerUseCase`/`DisconnectPeerUseCase` → `dispatchLifecycle`) synchronously waits on the room goroutine, which in `OnJoin`/`OnLeave` calls `EnetConnection.Ping` — a blocking call into the same loop; running dispatch on the executor would close a cross-deadlock executor↔room. `conn.Send`/`Close` — async enqueue into the loop's queue (FIFO, wire order preserved). `Event` accessors (`GetType`/`GetPeer`/`GetPacket`/`GetData`) work on the Go copy and need no dispatch. Only the run goroutine touches the `sessions` map — no synchronization. Shutdown: `Stop` → `closeSessions` (graceful `DisconnectLater` enqueued) → `drainHost` (2 service iterations via `Call`) → `loop.Stop()` → drain → `host.Destroy`.

Without `enet`/cgo: `Listen()` — stub run (warn + clean exit, `Err() == nil`).

## Connection factory — tagged alias `Peer`

`EnetConnectionFactory` (`port/transport/enet`, adapter `adapter/transport/enet`) — a delivery transport detail: the real seam with the service is `realtime.PeerFactory`, which accepts a ready-made `transport.Connection`. The factory interface is the same across all build tags — `CreateConnection(peer Peer, incoming chan domain.Frame)`, where `Peer` is a tagged type alias: under `enet && cgo` it is `libenet.Peer` (the `CreateConnection(peer, incoming)` call in `session_cgo.go` is type-safe and unchanged), in the stub build — `any`.

```bash
CGO_ENABLED=1 go build -tags enet -o bin/rooms-enet ./cmd/rooms
CGO_ENABLED=1 go test -tags enet ./tests/delivery/enet/...
```

The admit/teardown flow uses the same `AdmitPeerUseCase` / `DisconnectPeerUseCase` as WS (P20): teardown runs with the `PeerSession` from admit (identity check — a stale EventDisconnect after resume does not evict the new peer, `ErrPeerStale` → no-op).
