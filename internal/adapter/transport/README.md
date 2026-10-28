# adapter/transport

Limits and shared data-plane logic (WS + ENet).

## Config

`TransportConfig`:

| Field | Default | Env |
|-------|---------|-----|
| `MaxIncomingFrameBytes` | 256 KiB | `TRANSPORT_MAX_INCOMING_FRAME_BYTES` |
| `RoomIncomingQueueSize` | 128 | `TRANSPORT_ROOM_INCOMING_QUEUE` (`0` = unbuffered) |
| `PeerOutboundQueueSize` | 128 | `TRANSPORT_PEER_OUTBOUND_QUEUE` |
| `EnetIncomingQueueSize` | 256 | `TRANSPORT_ENET_INCOMING_QUEUE` (full → **drop**, the host loop is not blocked) |
| `PeerIdleTimeout` | 30s | `TRANSPORT_PEER_IDLE_TIMEOUT` (`0` — mechanism disabled; negatives are a Validate error) |
| `PeerIdleSweepInterval` | 10s | `TRANSPORT_PEER_IDLE_SWEEP_INTERVAL` (`0` — sweep does not start; negatives are a Validate error) |
| `WSWriteDeadline` | 10s | `TRANSPORT_WS_WRITE_DEADLINE` (`0` — no deadline; negatives are a Validate error) |
| `PeerCloseDrainTimeout` | 1s | `TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT` (`0` — drain disabled, `peer.Stop` does not wait for the write loop; negatives are a Validate error) |

`MaxIncomingFrameBytes=0` — **no limit** on the incoming frame (dangerous, explicit opt-in; see `adapter/config/README.md`).

`PeerCloseDrainTimeout` — the budget ceiling for the drain-aware peer close (P30, see `adapter/realtime/README.md`): `peer.Stop` waits for the write loop to drain `outbound` no longer than this budget, then `conn.Close`; `0` — the mechanism is off (like `PeerIdleTimeout <= 0`), Stop goes to close immediately.

**Overflow:** `room.incoming`, `peer.outbound` and the ENet session — **drop** the frame + warn (room loop / host loop / peer read are not blocked).

The ENet listener has a separate config — `enet.EnetConfig` (`enet/enet_config.go`, without build tags — stub builds need it too): `ListenPorts` (1..N UDP ports of host shards, P18), `PeerLimit` (per-host), `ChannelLimit`, `ServiceTimeoutMs`, `AdmitTimeout` + copies of `MaxIncomingFrameBytes`/`EnetIncomingQueueSize` from `TransportConfig` (in cmd/rooms). `ENET_*` env vars — `adapter/config.LoadFromEnv()` → `cfg.Enet`.

## Idle eviction (ADR 0001)

`Connection.IsAlive()` — the liveness contract: `false` → the peer is subject to idle eviction (`EvictIdlePeersUseCase` removes it via `DisconnectPeer`, a silent `Close`).

- **WS**: `lastSeen` (UnixNano, atomic) is updated on a successful incoming frame and on a pong; `Send` does not count. `IsAlive() = false` if `closed` or `lastSeen` is older than `PeerIdleTimeout`. `PeerIdleTimeout == 0` — only `closed` (negatives are rejected by `Validate`).
- **ENet**: an atomic flag cleared on `Close()`. Zombies are cleaned up by ENet's own timeout (`EventDisconnect` → `DisconnectPeer`); the flag exists for the unified contract.

## Wire

Parsing an incoming frame — `domain.DecodeIncomingFrame` (`OpCode` + payload).  
WS: `SetReadLimit` + decode in `adapter/transport/ws`.  
ENet: checked in `delivery/enet` before admit and in `frameFromPacket`.

On limit breach — **disconnect** without a wire error.

## Connection factories — a transport detail

The single seam between delivery and the service is `realtime.PeerFactory` (`CreatePeer(ctx, id, nick, transport.Connection, logger)`): the service does not care whether the connection is WS or ENet underneath. The peer does not store its room — at `Start` it receives the narrow `realtime.EventSink` (P26). `EnetConnectionFactory`/`WsConnectionFactory` remain in `port/transport/{enet,ws}` for DI in handlers and testability — a detail of the concrete transport, not a domain contract.

`EnetConnectionFactory` has a single untagged signature `CreateConnection(peer Peer, incoming chan domain.Frame)`: `Peer` is a tagged type alias (`port/transport/enet/peer_{cgo,stub}.go`) that resolves to `libenet.Peer` under `enet && cgo`, otherwise to `any`. The "tagged alias" pattern removes build-tag interface splitting without losing type safety on the cgo branch; the stub implementation ignores the arguments.

## ENet: libenet serialization (`HostLoop`)

The libenet C API is not thread-safe; only the `HostLoop` goroutine touches the host, its peers and packets (`enet/hostloop_cgo.go`, P15) — an executor draining a FIFO command queue. `NewHostLoop` creates the host inside itself: owners (server `delivery/enet` run, test clients) submit work via blocking commands `Call[T](loop, fn)`/`loop.Do(fn)` — including `host.Service` as a command (the client pump drives its own service itself); `EnetConnection` sends `Send`/`Close` async through `loop.enqueue`, `Ping` via a blocking `Call`. There is no global serialization — only submits of a single host contend; worst-case send latency is still ~`ServiceTimeoutMs`, but the convoy between hosts is gone.

Liveness invariant: the executor only runs time-bounded libenet calls and never waits on domain goroutines — a blocking `Call`/`Do` therefore cannot deadlock. Domain work that can block (admit/teardown → `AdmitPeer`/`DisconnectPeer` → `dispatchLifecycle` → waiting on the room goroutine) runs outside loop commands (delivery dispatch happens on the run goroutine); otherwise a `Ping` from room context would close an executor↔room cross-deadlock. `Event` accessors read a Go copy of the event — no confinement needed. `loop.Stop()` closes admission of blocking commands → drains the queue → `host.Destroy`; submits after stop get `ErrHostLoopStopped`, waiting callers unblock via execution or an error.

`EnetConnection.Close` queues a graceful `DisconnectLater` on the loop (FIFO preserves the wire order of queued packets before the disconnect); `Send`/`Ping` after `Close` return `io.EOF`/`-1` per the `alive` flag.

**go-enet quirk:** `peer.SendBytes`/`SendPacket` ignore the `enet_peer_send` return code — `Send` into a disconnecting/zombie peer silently returns nil. The break surfaces later as `EventDisconnect` → `DisconnectPeer`; the `alive` flag only cuts off explicitly closed conns.
