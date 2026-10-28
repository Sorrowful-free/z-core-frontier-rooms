# internal/loadgen

Load client for a fleet of rooms (plan [P27](../../docs/plans/P27-loadgen.md), inc 1–3: host/WS + Docker/netem + ENet client). Binary — `cmd/loadgen`, orchestrator — `scripts/loadtest-run.sh` (starts `cmd/rooms` with the [.env.loadtest](../../.env.loadtest) preset, captures `GET /metrics` before/after into `report.server_metrics`).

## Flow

`fleet.Run(ctx)`: ramp (`POST /rooms` → `POST /rooms/:id/tickets` × occupancy-1 → dial all clients) → sustain until `LOADGEN_DURATION` → drain (abrupt close of all conns → poll `rooms_active_rooms` + `rooms_reservation_active_slots` to zero with a deadline) → `report.json`. Rooms are not deleted — the drain itself is the check that "the server leaves no empty rooms". SIGINT → partial report (`interrupted: true`).

## Peer

`DataConn` (`Send`/`Recv`/`Close`) — a transport-agnostic OpCode+payload frame; implementations — WS (`fasthttp/websocket`) and ENet (`client_enet.go`, `-tags enet`). A read-loop is mandatory: the server answers ping only when the client is reading — a dead reader = idle eviction. Join = first frame `0x01`; before it a one-byte `0x40–0x7F` — reject. The client keeps a `RoomState` replica (codec) — peerID by nick, `is_master` → send entities. mode=state: `PatchInput` to everyone (`INPUT_HZ`), `PatchEntities` to the master (`ENTITIES_HZ`); occupancy — just stay alive. Resume: `?token=<reconnect_token>` after an abrupt close (clients from `RECONNECT_RATE`, period `RECONNECT_EVERY`).

## Env (LOADGEN_*)

| Variable | Default | Description |
|----------|---------|-------------|
| `LOADGEN_HTTP_ADDR` | `127.0.0.1:3000` | Server REST (host:port, no scheme) |
| `LOADGEN_WS_ADDR` | `=HTTP_ADDR` | WS endpoint (`/ws?token=`) |
| `LOADGEN_ENET_ADDR` | host `WS_ADDR` | ENet listeners host (no port — port comes from ticket `enet_port`) |
| `LOADGEN_ADMIN_ADDR` | `127.0.0.1:9091` | admin listener `GET /metrics` — drain-poll |
| `LOADGEN_ROOMS` | `1000` | Target room count |
| `LOADGEN_ROOM_MIX` | `4/4:70,8/6:20,16/4:10` | `capacity/occupancy:weight`, occupancy ≤ capacity ≤ 127 |
| `LOADGEN_TRANSPORT_MIX` | `ws:100` | Shares of transports `ws`/`enet`; `enet` requires a build with `-tags enet` (otherwise → `dial_errors{transport=enet}`) |
| `LOADGEN_MODE` | `state` | `state` — send state frames; `occupancy` — just stay alive |
| `LOADGEN_INPUT_HZ` | `20` | PatchInput per client, Hz |
| `LOADGEN_ENTITIES_HZ` | `20` | PatchEntities per master client, Hz |
| `LOADGEN_RAMP` | `10` | Rooms/sec during ramp-up |
| `LOADGEN_DURATION` | `60s` | Sustain after ramp |
| `LOADGEN_DRAIN_TIMEOUT` | `60s` | Drain-poll deadline (~2×max(idle=30s, sweep=10s)) |
| `LOADGEN_RECONNECT_RATE` | `0` | Fraction of clients with periodic abrupt close → resume |
| `LOADGEN_RECONNECT_EVERY` | `60s` | Drop period for selected clients (first one — with random jitter) |
| `LOADGEN_REPORT` | `./report.json` | report.json path (orchestrator sets `.scratch/loadtest/`) |
| `LOADGEN_LOG_EVERY` | `10s` | Live-progress period on stdout |
| `LOADGEN_NETEM_SHARE` | empty | `clean:30,bad:50,ugly:20` — client shares across netem profiles; empty = no SO_MARK (host mode) |
| `LOADGEN_NETEM_BAD` | `loss 5% delay 150ms 50ms` | `tc netem` argument string for band bad (read by the container entrypoint) |
| `LOADGEN_NETEM_UGLY` | `loss 20% delay 300ms 150ms reorder 10%` | Same for ugly |
| `LOADGEN_NETEM_DEV` | `eth0` | Outbound interface for tc in the container |

## Netem (inc 2, container)

Mechanism: at spawn the client gets a weighted pick from `NETEM_SHARE` → profile → `SO_MARK` on the dial socket (`netem.go`: clean=1, bad=2, ugly=3); tc in the container entrypoint — `prio` qdisc, `fw` filters `handle N → flowid 1:N`, netem child on bands bad/ugly (unmarked traffic → 1:1 clean). Control-plane HTTP is marked with the same pick on each new Transport conn. `SO_MARK` requires `cap_add: NET_ADMIN` — container mode only; do not set share on the host (dial will fail EPERM → `dial_errors`). Share verification: `report.netem_clients` vs `report.config.netem_share`, contract ±5%.

```bash
# orchestrator docker mode: build --network=host (bridge DNS is dead),
# server on the host, container — via host.docker.internal, report → $OUT_DIR.
LOADTEST_DOCKER=1 LOADGEN_ROOMS=30 LOADGEN_ROOM_MIX="4/4:100" \
LOADGEN_NETEM_SHARE="clean:30,bad:50,ugly:20" LOADGEN_DURATION=30s \
./scripts/loadtest-run.sh
```

Manual run: `docker compose -f deploy/loadgen/docker-compose.loadgen.yml run --rm loadgen` (the image can only be built with `docker build --network=host` — compose build cannot do host-net).

## Report

`report.json`: join/dial latency p50/p95/p99, frames in/out, counters by opcode (reject 0x40–0x7F, in-room 0x60–0x6F, notifications 0x80–0x82), reconnect attempts/success, `netem_clients` — actual profile shares, `drain_complete` + `rooms_dump` on incomplete drain, `server_metrics` — exposition snapshots before/after (merged by the orchestrator). No pass/fail verdict.

## Smoke

```bash
LOADGEN_ROOMS=4 LOADGEN_ROOM_MIX="4/4:100" \
LOADGEN_TRANSPORT_MIX="ws:100" LOADGEN_DURATION=30s LOADGEN_RECONNECT_RATE=0.2 \
./scripts/loadtest-run.sh
```

## ENet (inc 3, `-tags enet`)

`enetDataConn` (`client_enet.go`, `//go:build enet && cgo`) — a client-side libenet host under `adapterenet.HostLoop` (all libenet calls via `Call`/`Do`, same as the server side); without the tag — `client_enet_stub.go` returns `ErrEnetUnsupported` on dial. Dial: `Connect(LOADGEN_ENET_ADDR:enet_port)` — the port comes **strictly** from the ticket response (`enet_port`, strict binding P18 — the server does the distribution across `ENET_LISTEN_PORTS`, `enet_port=0` → dial error); the pump waits for `EventConnect` within the peer's dial timeout, then sends the admit packet — the **raw decoded ticket** on channel 0 reliable (WS passes base64url in query, on ENet the client decodes). Frames — `[op|payload]` channel 0 reliable in both directions. Resume with a reconnect token works the same way (dial → admit).

Known limitation: **SO_MARK/netem does not apply to ENet** — the go-enet host owns the UDP socket and does not expose the fd; enet traffic leaves unmarked → in the container it lands in the clean tc band (in `netem_clients` enet peers are still counted under their assigned profile).

Docker smoke (the `zcore-loadgen` image builds `/app/loadgen` with `-tags enet`; the server is on the network under alias `rooms`, `ENET_LISTEN_PORTS` ports from the preset):

```bash
docker build --network=host -t zcore-loadgen -f deploy/loadgen/Dockerfile .
LOADTEST_DOCKER=1 LOADGEN_ENET_ADDR=rooms LOADGEN_ROOMS=10 \
LOADGEN_ROOM_MIX="4/4:100" \
LOADGEN_TRANSPORT_MIX="ws:70,enet:30" LOADGEN_DURATION=15s \
./scripts/loadtest-run.sh
# expected: join_success ≈ spawned, dial_errors{transport=enet} = 0, drain_complete=true
```
