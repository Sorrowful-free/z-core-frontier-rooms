# adapter/metrics

Prometheus-реализация [`port/metrics`](../../port/metrics/README.md) (P19).

## API

- `New(cfg MetricsConfig) (*Metrics, error)` — собирает собственный `prometheus.Registry` (не default: без чужих коллекторов), регистрирует сигналы + стандартные `go_*`/`process_*` коллекторы.
- `Gatherer()` — registry для exposition; `delivery/admin` отдаёт его по `GET /metrics`.

## MetricsConfig

- `AdminListenAddr` — адрес отдельного admin Fiber listener'а (env `ADMIN_LISTEN_ADDR`). **Пустой = endpoint выключен** (дефолт; scrape надо включать явно). Валидация — `net.ResolveTCPAddr`.

## Сигналы (namespace `rooms`)

| Метрика | Тип | Label |
|---------|-----|-------|
| `rooms_active_rooms` | gauge | — |
| `rooms_active_peers` | gauge | — |
| `rooms_peer_joins_total` / `rooms_peer_leaves_total` | counter | — |
| `rooms_join_rejects_total` | counter | `opcode` (hex, `0x40`…) |
| `rooms_queue_drops_total` | counter | `queue` = `room_incoming` / `peer_outbound` / `enet_incoming` |
| `rooms_master_elections_total` | counter | — |
| `rooms_reservation_ops_total` | counter | `op` = `reserve` / `admit` / `revoke` |
| `rooms_sweep_processed_total` | counter | `sweep` = `orphan_admitted` / `idle_peers` |
| `rooms_room_incoming_depth` / `rooms_peer_outbound_depth` | gauge | — |
| `rooms_room_tick_seconds` | summary | `kind` = `full` / `patch` |
| `rooms_peer_send_errors_total` | counter | — |
| `rooms_enet_host_peers` | gauge | `host` (HostID, bounded числом `ENET_LISTEN_PORTS`) |
| `rooms_enet_host_loop_queue_latency_seconds` | summary | — |
| `rooms_reservation_active_slots` | gauge | — |
| `rooms_sweep_runs_total` | counter | `sweep` = `orphan_admitted` / `idle_peers` |
| `rooms_http_request_duration_seconds` | histogram | `route` (шаблон), `method`, `status` |

Per-room/per-peer label'ы намеренно отсутствуют — кардинальность (P19). `peer_joins/peer_leaves` считают lifecycle-переходы: `Replace` даёт пару leave+join.

P19b-нюансы: `join_rejects` - только успешно отправленные reject-кадры; `master_elections` - первый элекшн и перевыборы (emits staged внутри транзакций policy, откат даёт редкий пересчёт); `reservation_ops{revoke}` - любое реальное удаление слота (Revoke, expired в Admit, lazy sweep); `*_depth` gauges - сэмплы `len(chan)` на enqueue/dequeue (last-write-wins приближение, не сумма по инстансам); `host_loop_queue_latency` агрегирован по host'ам (без label - очередь одного loop'а от остальных не отделить на одной метрике); `http_request_duration` - upgrade-запросы `/ws` пропускаются (время жизни соединения, а не запроса).

Env-индекс: [adapter/config/README.md](../config/README.md).
