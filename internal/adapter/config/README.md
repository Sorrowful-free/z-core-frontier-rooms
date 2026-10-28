# adapter/config

Process configuration assembled from env. See [.cursor/rules/config.mdc](../../../.cursor/rules/config.mdc).

## API

- `LoadFromEnv() (*Config, error)` — single loading entry point at startup.

## Structure

`Config` contains fields of types from the adapter packages of the modules (`Logging`, `Server`, `Admission`, `HTTPAuth`, `HTTPLimits`, `Reservation`, `StatePolicy`, `Transport`, `Enet`, `Metrics`).

## Env (process)

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `LOG_MODE` | no | `prod` | Root zap: `prod` or `dev` |
| `HTTP_ADDR` | no | `:3000` | `fiber.App.Listen` address |

Validation — `logging/zap.RootConfig.Validate()`, `server.ServerConfig.Validate()`.

## Env (admission)

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ADMISSION_SECRET` | yes | — | HMAC ticket secret, ≥ 32 bytes; `dev-secret-change-me` is forbidden |
| `ADMISSION_TTL` | no | `1h` | Ticket TTL (`time.ParseDuration`) |

Validation of admission fields — `admission.AdmissionConfig.Validate()`.

## Env (http auth)

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `HTTP_API_KEY` | yes* | — | REST API key; ≥ 16 bytes; `Authorization: Bearer` or `X-API-Key` |
| `HTTP_AUTH_DISABLED` | no | `false` | `true` — disable auth (local dev only) |

\* Not required if `HTTP_AUTH_DISABLED=true`.

Validation — `httpauth.HTTPAuthConfig.Validate()`.

## Env (http limits)

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MAX_ROOMS` | no | `500` | Max rooms per process; `0` — no limit |
| `HTTP_CREATE_RATE_PER_MIN` | no | `30` | `POST /rooms` per IP per minute; `0` — no limit |
| `HTTP_ISSUE_RATE_PER_MIN` | no | `60` | `POST /rooms/:id/tickets` per IP per minute; `0` — no limit |
| `HTTP_DELETE_RATE_PER_MIN` | no | `30` | `DELETE /rooms/:id` per IP per minute; `0` — no limit |
| `HTTP_LIST_RATE_PER_MIN` | no | `60` | `GET /rooms` per IP per minute; `0` — no limit |

Validation — `httplimits.HTTPLimitsConfig.Validate()`.

## Dependencies

`adapter/config` imports adapter modules (e.g. `adapter/admission`). The reverse import is forbidden.

## Env (reservation reconnect grace / orphan sweep)

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `RESERVATION_RECONNECT_TTL` | no | `30s` | Grace of the admitted slot from lastSeen (disconnect/resume) until revoke; `0` — immediately at sweep |
| `RESERVATION_ORPHAN_SWEEP_INTERVAL` | no | `10s` | Interval of background sweeps — orphan admitted **and** empty rooms (P31); `0` — disable both |

Validation — `reservation.ReservationConfig.Validate()`.

## Env (state room policy)

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `STATE_POLICY_ALLOW_MASTER_MIGRATION` | no | `true` | Deploy master-migration mode (P33): `false` — the room dies when the master leaves (`OnLeave` → `ErrRoomShouldClose` → teardown); a non-bool value — load error |

Validation — `realtime/policy/state.StatePolicyConfig.Validate()` (trivial, for ModuleConfig uniformity).

## Env (enet listener)

Part of `LoadFromEnv()` → `Config.Enet` (type `transport/enet.EnetConfig`).

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ENET_LISTEN_PORTS` | no | `7777` | CSV of ENet host (shard, P18) UDP ports: `7777,7778,...`; one HostLoop per port; 1–65535, no duplicates |
| `ENET_PEER_LIMIT` | no | `64` | Max simultaneous ENet peers **per host** |
| `ENET_ADMIT_TIMEOUT` | no | `10s` | connect→admit deadline; an overdue peer is disconnected |
| `ENET_SERVICE_TIMEOUT_MS` | no | `1` | host.Service iteration budget = ceiling of HostLoop command queue latency; >0 |

A host's (shard) `HostID` — index+1 in `ListenPorts`; it lands in the ticket claims for strict binding admit. Ticket issue balancing — `port/transport/enet.HostRegistry` (least-loaded by live peers, round-robin tie-break), implemented by `delivery/enet.RoomsHandler`.

Validation — `transport/enet.EnetConfig.Validate()` (called in `loadEnetFromEnv` and again in `cmd/rooms` after copying transport limits).

## Env (transport / data plane)

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TRANSPORT_MAX_INCOMING_FRAME_BYTES` | no | `262144` (256 KiB) | Max incoming WS/ENet frame and ENet admit packet; `0` — **no limit** (see below) |
| `TRANSPORT_ROOM_INCOMING_QUEUE` | no | `128` | `room.incoming` buffer; `0` — unbuffered; full → drop |
| `TRANSPORT_PEER_OUTBOUND_QUEUE` | no | `128` | `peer.outbound` buffer; `0` — unbuffered; full → drop |
| `TRANSPORT_ENET_INCOMING_QUEUE` | no | `256` | ENet session buffer; full → frame drop |
| `TRANSPORT_PEER_IDLE_TIMEOUT` | no | `30s` | `IsAlive` liveness window (WS lastSeen); `0` — disable idle eviction (negative — Validate error) |
| `TRANSPORT_PEER_IDLE_SWEEP_INTERVAL` | no | `10s` | Background idle peer sweep interval; `0` — disable (negative — Validate error) |
| `TRANSPORT_WS_WRITE_DEADLINE` | no | `10s` | WS frame write deadline (`SetWriteDeadline` before `WriteMessage`); `0` — no deadline (negative — Validate error) |
| `TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT` | no | `1s` | `peer.outbound` drain ceiling on `peer.Stop` (P30): frames committed before Stop get `Send` before `conn.Close`; `0` — drain off (negative — Validate error) |

`TRANSPORT_MAX_INCOMING_FRAME_BYTES=0` / `MaxIncomingFrameBytes=0` — **unlimited incoming frame**: neither `SetReadLimit` (WS) nor the packet size check (ENet) is applied. Dangerous (a single client can eat memory with a giant frame) — only as an explicit opt-in, not for a prod default.

Validation — `transport.TransportConfig.Validate()`.

Wiring — `cmd/rooms`: `LoadFromEnv()` → `ws.NewWsConnectionFactory(..., cfg.Transport)`; `cfg.Enet` → `enetCfg`, then `enetCfg.MaxIncomingFrameBytes = cfg.Transport.MaxIncomingFrameBytes`, `enetCfg.EnetIncomingQueueSize = cfg.Transport.EnetIncomingQueueSize`.

## Env (metrics / admin listener)

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ADMIN_LISTEN_ADDR` | no | `` (disabled) | Address of a separate admin Fiber listener with `GET /metrics` (prometheus exposition). Empty — the endpoint is not started |

The default is intentionally off: the scrape endpoint is opt-in, the port is not bound without an explicit env. Validation — `metrics.MetricsConfig.Validate()` (`net.ResolveTCPAddr`).

Wiring — `cmd/rooms`: `metricsadapter.New(cfg.Metrics)` → `port/metrics` into all emitters; when an address is set — `net.Listen` synchronously before the serve goroutine, then `admin.NewMetricsApp(gatherer).Listener(ln)` (P19b).
