# adapter/metrics

Prometheus implementation of [`port/metrics`](../../port/metrics/README.md) (P19).

## API

- `New(cfg MetricsConfig) (*Metrics, error)` — builds its own `prometheus.Registry` (not the default: no foreign collectors), registers the signals + standard `go_*`/`process_*` collectors.
- `Gatherer()` — registry for exposition; `delivery/admin` serves it at `GET /metrics`.

## MetricsConfig

- `AdminListenAddr` — address of a separate admin Fiber listener (env `ADMIN_LISTEN_ADDR`). **Empty = endpoint off** (default; scraping must be enabled explicitly). Validation — `net.ResolveTCPAddr`.

## Signals (namespace `rooms`)

| Metric | Type | Label |
|--------|------|-------|
| `rooms_active_rooms` | gauge | — |
| `rooms_active_peers` | gauge | — |
| `rooms_peer_joins_total` / `rooms_peer_leaves_total` | counter | — |
| `rooms_peer_disconnects_total` / `rooms_peer_reconnects_total` | counter | — |
| `rooms_join_rejects_total` | counter | `opcode` (hex, `0x40`…) |
| `rooms_queue_drops_total` | counter | `queue` = `room_incoming` / `peer_outbound` / `enet_incoming` |
| `rooms_master_elections_total` | counter | — |
| `rooms_reservation_ops_total` | counter | `op` = `reserve` / `admit` / `revoke` |
| `rooms_sweep_processed_total` | counter | `sweep` = `orphan_admitted` / `idle_peers` / `empty_rooms` |
| `rooms_room_incoming_depth` / `rooms_peer_outbound_depth` | gauge | — |
| `rooms_room_tick_seconds` | summary | `kind` = `full` / `patch` |
| `rooms_peer_send_errors_total` | counter | — |
| `rooms_enet_host_peers` | gauge | `host` (HostID, bounded by the `ENET_LISTEN_PORTS` count) |
| `rooms_enet_host_loop_queue_latency_seconds` | summary | — |
| `rooms_reservation_active_slots` | gauge | — |
| `rooms_sweep_runs_total` | counter | `sweep` = `orphan_admitted` / `idle_peers` / `empty_rooms` |
| `rooms_http_request_duration_seconds` | histogram | `route` (template), `method`, `status` |

Per-room/per-peer labels are intentionally absent — cardinality (P19). `peer_joins/peer_leaves` count lifecycle transitions: `Replace` produces a leave+join pair.

P19b nuances: `join_rejects` — only successfully sent reject frames; `master_elections` — the first election and re-elections (emits staged inside policy transactions, a rollback gives a rare over-count); `reservation_ops{revoke}` — any real slot removal (Revoke, expired in Admit, lazy sweep); `*_depth` gauges — `len(chan)` samples on enqueue/dequeue (last-write-wins approximation, not a sum across instances); `host_loop_queue_latency` aggregated across hosts (without a label, a single loop's queue cannot be told apart from the others on one metric); `http_request_duration` — `/ws` upgrade requests are skipped (connection lifetime, not request time).

Env index: [adapter/config/README.md](../config/README.md).
