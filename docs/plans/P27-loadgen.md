# P27 — loadgen: нагрузочный клиент (комнаты × клиенты, плохая сеть, метрики)

## Контекст

Цель: бинарь, имитирующий сотни–тысячи комнат с реальными клиентами
(смесь capacity/occupancy, дефолт-цель 1000 комнат), плохим сетевым
профилем и сбором метрик. Готового tooling нет; ближайший клиентский
код — `tests/integration/match_e2e_*.go` (`matchClientBase`,
`wsClient`, `enetClient`), но он привязан к `*testing.T` и не пригоден
для standalone-бинаря без экстракции.

Факты, от которых пляшет дизайн:

- Join-флоу: `POST /rooms` (создаёт комнату + host-тикет) →
  `POST /rooms/:id/tickets` для остальных → `GET /ws?token=...` →
  первый фрейм `0x01` FullState = «в комнате» (явного join-OK нет;
  фейл — один байт-OpCode reject 0x40–0x5F/0x70–0x7F и close).
- ENet: первый пакет после EVENT_CONNECT — сырой декодированный тикет
  (reliable, ch0), порт — `enet_port` из ticket-ответа.
- WS-клиент обязан читать постоянно: `fasthttp/websocket` отвечает на
  ping только внутри `ReadMessage`; мёртвый reader → idle-eviction
  через `TRANSPORT_PEER_IDLE_TIMEOUT` (30s default).
- Codec (`internal/adapter/realtime/codec`) pure Go и под `internal/` —
  инструмент живёт в этом репо.
- ENet-клиент = cgo + libenet → сборка под `-tags enet` в образе
  `zcore-rooms-dev` (`.devcontainer/Dockerfile`), паттерн — как у
  `scripts/enet-verify.sh`.
- Лимиты по умолчанию душат прогон: `MAX_ROOMS=500`,
  `HTTP_CREATE_RATE_PER_MIN=30`, `HTTP_ISSUE_RATE_PER_MIN=60`
  (per-IP!) — для 1000 комнат × 4 клиента нужен env-пресет.
- `POST /rooms` принимает `policy` (P29): `state` (дефолт,
  master-миграция) | `authoritative-host` (уход хоста = смерть
  комнаты: оставшимся `0x82 OpRoomClosed` + disconnect, слоты
  ревокаются). Неизвестное значение → `400 invalid_policy`.
- Reconnect-credential — отдельный kind=2 тикета (P28, строгие kind:
  join=1, reconnect=2); клиенту тикет opaque — join-флоу loadgen'а
  не меняется, reconnect идёт тем же `?token=` с reconnect-токеном
  из ticket-ответа.
- P30 (drain-aware close) идёт параллельной веткой: до его мержа
  0x82 на WS — best-effort. Loadgen считает фактически полученные
  `0x82` (метрика `room_closed_received`) — наблюдение в отчёте,
  НЕ pass/fail: промахи до P30 — известный baseline, после — регресс.
- Сервер уже отдаёт Prometheus на `ADMIN_LISTEN_ADDR` → `GET /metrics`:
  `rooms_active_rooms`, `rooms_active_peers`,
  `rooms_queue_drops_total{queue}`, `rooms_room_tick_seconds{kind}`,
  `rooms_peer_{joins,leaves,disconnects,reconnects}_total`,
  `rooms_join_rejects_total{opcode}`, `rooms_reservation_active_slots`.

## Решения

### Топология

```text
host:                     docker:
┌──────────────────┐      ┌─────────────────────────────┐
│ rooms (cmd/rooms)│◄─────│ loadgen container           │
│  :3000 HTTP/WS   │ data │  - tc netem на egress       │
│  :5000+ ENet     │      │  - fleet: ws+enet clients   │
│  :9091 admin     │◄─ ─ ─│  (admin scrape — с хоста,   │
└──────────────────┘      │   мимо impairment)          │
     ▲                    └─────────────────────────────┘
     └─ orchestrator script (host): поднимает preset-env,
        гоняет прогон, снимает /metrics snapshot до/после
```

- Сервер — **внешний адрес** (`LOADGEN_HTTP_ADDR`, `LOADGEN_WS_ADDR`,
  `LOADGEN_ENET_PORTS`), не subprocess: можно гонять против хоста или
  docker-контейнера.
- netem — **внутри контейнера loadgen** (`tc qdisc` на egress в
  entrypoint). HTTP-трафик create/issue тоже идёт через impair —
  реалистично (клиенты за плохой сетью и на control-plane); admin
  не трогаем, т.к. scrape идёт с хоста.
- Docker build — `--network=host` обязателен (DNS в bridge на этой
  машине мёртв — см. global rules).

### Флот и профили

- Один процесс loadgen, env-конфиг:
  - `LOADGEN_ROOMS=1000` — целевое число комнат.
  - `LOADGEN_ROOM_MIX="4/4:70,8/6:20,16/4:10"` — capacity/occupancy:вес%
    (capacity ≤ `domain.MaxRoomCapacity`=127).
  - `LOADGEN_TRANSPORT_MIX="ws:70,enet:30"` — доля клиентов на
    транспорт; без `-tags enet` enet-доля фейлится на dial (метрика
    `dial_errors{transport=enet}`).
  - `LOADGEN_POLICY_MIX="state:80,auth:20"` — доля комнат, создаваемых
    с `policy=authoritative-host` (P29). У auth-комнат host-клиент в
    sustain-фазе делает **одноразовый abrupt disconnect** в случайный
    момент (без reconnect — хоста нет, комната мертва); остальные
    клиенты комнаты ожидают `0x82` до обрыва (счётчик
    `room_closed_received` vs `auth_rooms_died`) и завершаются —
    комнаты больше нет. Проверяет closeRoom/drain на масштабе и
    drain-complete по смерти хоста (комната исчезает из
    `rooms_active_rooms` + слотов).
  - `LOADGEN_MODE=state|occupancy` — в state: все шлют `PatchInput`
    (`LOADGEN_INPUT_HZ`, default 20), host — `PatchEntities`
    (`LOADGEN_ENTITIES_HZ`, default 20); occupancy — только жить.
  - `LOADGEN_RAMP=10` — комнат/сек при наполнении.
  - `LOADGEN_DURATION`, `LOADGEN_DRAIN_TIMEOUT`.
  - `LOADGEN_RECONNECT_RATE` — доля клиентов, делающих abrupt
    disconnect → `?token=reconnect_token` каждые
    `LOADGEN_RECONNECT_EVERY` (default 60s) — стресс resume-пути
    (0x80/0x81, Replace, grace-гейт P24).
- Никнеймы уникальные (`lg-<n>`), иначе `ticket_slot_held`.
- Каждый клиент: обязательный read-loop (см. факт про ping/pong);
  host-клиент = мастер (шлёт entities), остальные — input.
- Комнаты **не удаляются** после прогона: drain-фаза — все клиенты
  отваливаются (abrupt close), loadgen поллит `rooms_active_rooms` +
  `rooms_reservation_active_slots` до нуля с дедлайном
  ~2×max(`TRANSPORT_PEER_IDLE_TIMEOUT`, `RESERVATION_ORPHAN_SWEEP_INTERVAL`).
  Не дошло до 0 → `GET /rooms` дамп в отчёт. Это и есть проверка
  «сервер не оставляет пустые комнаты».

### Плохая сеть (netem)

- Entrypoint контейнера: `tc qdisc add dev eth0 root handle 1: prio`
  + два child-qdisc'а netem и фильтры по dst-port / class.
- Пресеты (env-оверрайды `LOADGEN_NETEM_*`):
  - `bad`: loss 5%, delay 150ms±50ms.
  - `ugly`: loss 20%, delay 300ms±150ms, reorder 10%.
- Распределение клиентов по пресетам — per-client через
  `LOADGEN_NETEM_SHARE="clean:30,bad:50,ugly:20"`: чистый пер-client
  tc не масштабируется на тысячи, поэтому реализация — **исходящие
  fwmark/classid по src-port клиента не делаем**; вместо этого
  соотношение аппроксимируется тремя «групповыми» egress-правилами
  (по диапазонам local ephemeral ports, выделяемым клиентам при
  `net.Dialer.Control` SO_MARK/fwmark → tc filter fw). Деталь — на
  реализации; контракт плана: доли из `NETEM_SHARE` должны
  статистически совпадать (±5%) с фактическими по метрикам клиентов.

### Метрики

- `report.json` по завершении (путь `LOADGEN_REPORT`, default
  `/out/report.json`): join success rate, dial/join latency p50/p95/p99,
  frames in/out per client & total, PatchState received rate, errors
  by type/opcode, reconnect attempts/success, evictions observed,
  `room_closed_received`/`auth_rooms_died` (0x82-доставка — см. P30).
- Live-прогресс на stdout раз в `LOADGEN_LOG_EVERY` (default 10s):
  rooms up, clients up, msg rates, error counters.
- Server scrape: orchestrator (host-скрипт) снимает `GET /metrics`
  до и после прогона, дельта складывается в `report.server_metrics`.
- **Без pass/fail-вердикта** — чистый сбор; drain-дедлайн — событие
  в отчёте (`drain_complete: false` + дамп), не exit code.

### Env-пресет сервера

Файл `.env.loadtest` (загружается оркестратором, не коммитить секреты):

```text
HTTP_AUTH_DISABLED=true
MAX_ROOMS=0
HTTP_CREATE_RATE_PER_MIN=0
HTTP_ISSUE_RATE_PER_MIN=0
HTTP_DELETE_RATE_PER_MIN=0
HTTP_LIST_RATE_PER_MIN=0
ADMIN_LISTEN_ADDR=:9091
TRANSPORT_ROOM_INCOMING_QUEUE=512
TRANSPORT_PEER_OUTBOUND_QUEUE=512
TRANSPORT_ENET_INCOMING_QUEUE=1024
ENET_PEER_LIMIT=4095
ENET_LISTEN_PORTS=5000-5003
ADMISSION_SECRET=<32+ bytes, генерится оркестратором>
```

### Клиентская реализация

- `internal/loadgen/client` — транспорт-агностичный интерфейс
  `DataConn` (`Send(op, payload) error`, `Recv() (op, payload, error)`,
  `Close()`) поверх WS (`fasthttp/websocket` client) и ENet
  (`go-enet` HostLoop per client, ticket-first-packet — паттерн
  `tests/integration/match_e2e_enet_test.go`).
- `internal/loadgen/peer` — сценарный движок клиента: read-loop →
  decode 0x01/0x02 через codec, лог 0x07/0x60+/0x80/0x81; send-loop
  по режиму; reconnect-логика по расписанию; вся телеметрия —
  через атомики/важные счётчики в `internal/loadgen/metrics`.
- HTTP control-plane — `fasthttp.Client` или stdlib с retry и
  пер-операционными таймаутами; ошибки HTTP ≠ ошибки join —
  раздельные счётчики.
- Lifecycle: `fleet.Run(ctx)` → ramp-создание комнат (host create →
  issue × (occupancy-1) → dial всех) → sustain до `DURATION` →
  drain (close all) → poll-drain → report. Graceful: ctx-cancel
  на SIGINT — отчёт всё равно пишется (partial).

## Инкременты

План большой — исполнение тремя инкрементами; каждый — отдельный заход
со своей верификацией. Скоуп правок ниже разбит по инкрементам;
зафиксированный скоуп override (ai-companion) — сумма трёх списков.

### Inc 1 — host-only core (WS, без Docker/cgo)

Всё, что гоняется на хосте без docker и без `-tags enet`:

- `cmd/loadgen/main.go` — entrypoint, env-конфиг, запуск fleet.
- `internal/loadgen/` — `config.go`, `client.go` (интерфейс `DataConn` +
  WS-реализация на `fasthttp/websocket`), `peer.go`, `fleet.go`,
  `metrics.go`, `report.go`.
- `internal/loadgen/README.md` — пакетная документация (расширять по
  инкрементам).
- `.env.loadtest` — пресет (в `.gitignore`-проверку: без секретов —
  secret генерится рантайм).
- `scripts/loadtest-run.sh` — оркестратор в **host-режиме**: поднимает
  `cmd/rooms` с preset-env (`go run` / бинарь), гоняет `go run
  ./cmd/loadgen`, снимает `/metrics` до/после, складывает report.
- Фичи inc 1: `ROOM_MIX`, `POLICY_MIX` (+ host-kill auth-комнат,
  счётчик `room_closed_received`), `TRANSPORT_MIX` (ws-only; enet-доля
  → `dial_errors`), `INPUT_HZ`/`ENTITIES_HZ`, `RAMP`, `DURATION`,
  `DRAIN_TIMEOUT`, `RECONNECT_RATE`/`RECONNECT_EVERY` (WS resume по
  `?token=` reconnect-токеном), `report.json`, live-прогресс, server
  metrics snapshot.

Верификация inc 1:

- `go build ./...`, `~/go/bin/golangci-lint run ./...` — 0.
- Smoke на хосте (без docker): оркестратор поднимает rooms с
  `.env.loadtest`, loadgen `LOADGEN_ROOMS=4
  LOADGEN_ROOM_MIX="4/4:100" LOADGEN_POLICY_MIX="state:50,auth:50"
  LOADGEN_TRANSPORT_MIX="ws:100" LOADGEN_DURATION=30s
  LOADGEN_RECONNECT_RATE=0.2` → `report.json` существует и валиден,
  `drain_complete: true`, `join_success ≥ rooms×occupancy`,
  `auth_rooms_died ≥ 1`, `room_closed_received` засчитан
  (промахи ок — pre-P30 baseline, см. Контекст).

### Inc 2 — Docker + netem

- `deploy/loadgen/Dockerfile` — FROM zcore-rooms-dev + `tc`/`iproute2`,
  entrypoint с netem.
- `deploy/loadgen/docker-compose.loadgen.yml` — сервис loadgen
  (cap_add: NET_ADMIN), опц. сервер как сервис.
- `scripts/loadtest-run.sh` — docker-режим: build `--network=host`,
  запуск контейнера, сбор report из `/out`.
- `internal/loadgen/README.md` — профили `bad`/`ugly`, `NETEM_SHARE`.

Верификация inc 2:

- `docker build --network=host` — образ собирается.
- Контейнерный прогон с `bad`/`ugly` пресетами → `report.json`;
  доли `NETEM_SHARE` статистически совпадают ±5% (по метрикам клиентов).

### Inc 3 — ENet-клиент

- `internal/loadgen/client_enet.go` (`//go:build enet && cgo`) +
  `client_enet_stub.go` (без тега → dial error).
- Распределение по `ENET_LISTEN_PORTS` (round-robin по `enet_port`
  из ticket-ответа); `TRANSPORT_MIX` enet-доля.
- `internal/loadgen/README.md` — enet-секция.

Верификация inc 3:

- `./scripts/enet-verify.sh build` + `vet` — tagged-код компилится.
- Smoke внутри `zcore-rooms-dev`: `TRANSPORT_MIX="ws:70,enet:30"` →
  `dial_errors{transport=enet}=0`, join_success по обоим транспортам.

Вне скоупа (всех инкрементов): правки production-кода сервера, правки
`tests/` (кроме опциональных тестов loadgen по запросу), изменения
CI-workflow.

## Открытые детали реализации (не блокируют план)

- Точный механизм netem-шардинга per-client (fwmark vs port-ranges) —
  inc 2.
- PatchInput/PatchEntities payload — минимально валидные по codec
  (подсмотреть в `match_e2e_client_test.go`).
- Распределение ENet-клиентов по `ENET_LISTEN_PORTS` (round-robin
  по ответному `enet_port`) — inc 3.

## Верификация

По инкрементам — см. раздел «Инкременты». Финальный прогон после inc 3:

- `go build ./...` — 0 ошибок.
- `./scripts/enet-verify.sh build` **и** `vet` — tagged-код компилится
  (обязательно: `client_enet.go` под тегом).
- `~/go/bin/golangci-lint run ./...` — 0 issues.
- Полный smoke: сервер с `.env.loadtest` + контейнер loadgen
  `LOADGEN_ROOMS=10 LOADGEN_ROOM_MIX="4/4:70,8/6:30"
  LOADGEN_POLICY_MIX="state:80,auth:20" LOADGEN_TRANSPORT_MIX="ws:70,enet:30"
  LOADGEN_DURATION=60s` → `report.json` валиден, `drain_complete: true`,
  `join_success ≥ rooms×occupancy`, поля `server_metrics` и
  `client_metrics` присутствуют.
