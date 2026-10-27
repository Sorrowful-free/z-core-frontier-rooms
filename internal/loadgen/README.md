# internal/loadgen

Нагрузочный клиент флота комнат (план [P27](../../docs/plans/P27-loadgen.md), inc 1+2: host/WS + Docker/netem). Бинарь — `cmd/loadgen`, оркестратор — `scripts/loadtest-run.sh` (поднимает `cmd/rooms` с пресетом [.env.loadtest](../../.env.loadtest), снимает `GET /metrics` до/после в `report.server_metrics`).

## Поток

`fleet.Run(ctx)`: ramp (`POST /rooms` с `policy` → `POST /rooms/:id/tickets` × occupancy-1 → dial всех клиентов) → sustain до `LOADGEN_DURATION` → drain (abrupt close всех conn → poll `rooms_active_rooms` + `rooms_reservation_active_slots` до нуля с дедлайном) → `report.json`. Комнаты не удаляются — drain и есть проверка «сервер не оставляет пустых комнат». SIGINT → partial-отчёт (`interrupted: true`).

## Peer

`DataConn` (`Send`/`Recv`/`Close`) — транспорт-агностичный кадр OpCode+payload; WS-реализация на `fasthttp/websocket` (`DialParams` уже несёт `EnetAddr`/`EnetPort` для inc 3). Обязательный read-loop: сервер отвечает ping только при читающем клиенте — мёртвый reader = idle-eviction. Join = первый кадр `0x01`; до него однобайтовый `0x40–0x7F` — reject. Клиент держит реплику `RoomState` (codec) — peerID по nick'у, `is_master` → send entities. mode=state: `PatchInput` всем (`INPUT_HZ`), `PatchEntities` master'у (`ENTITIES_HZ`); occupancy — только жить. Resume: `?token=<reconnect_token>` после abrupt close (клиенты из `RECONNECT_RATE`, период `RECONNECT_EVERY`).

Auth-комнаты (`POLICY_MIX=auth`): host-клиент делает одноразовый abrupt disconnect в случайной точке sustain без reconnect — комната умирает (`auth_rooms_died`); остальные считают полученные `0x82` (`room_closed_received`, доставка до P30 — baseline, не pass/fail).

## Env (LOADGEN_*)

| Переменная | Default | Описание |
|------------|---------|----------|
| `LOADGEN_HTTP_ADDR` | `127.0.0.1:3000` | REST сервера (host:port, без схемы) |
| `LOADGEN_WS_ADDR` | `=HTTP_ADDR` | WS endpoint (`/ws?token=`) |
| `LOADGEN_ADMIN_ADDR` | `127.0.0.1:9091` | admin listener `GET /metrics` — drain-poll |
| `LOADGEN_ROOMS` | `1000` | Целевое число комнат |
| `LOADGEN_ROOM_MIX` | `4/4:70,8/6:20,16/4:10` | `capacity/occupancy:вес`, occupancy ≤ capacity ≤ 127 |
| `LOADGEN_TRANSPORT_MIX` | `ws:100` | Доли транспортов; `enet` на inc1 → `dial_errors{transport=enet}` |
| `LOADGEN_POLICY_MIX` | `state:100` | `state` / `auth` (= `authoritative-host`), веса |
| `LOADGEN_MODE` | `state` | `state` — шлём state-кадры; `occupancy` — только жить |
| `LOADGEN_INPUT_HZ` | `20` | PatchInput на клиента, Гц |
| `LOADGEN_ENTITIES_HZ` | `20` | PatchEntities на master-клиента, Гц |
| `LOADGEN_RAMP` | `10` | Комнат/сек при наполнении |
| `LOADGEN_DURATION` | `60s` | Sustain после ramp |
| `LOADGEN_DRAIN_TIMEOUT` | `60s` | Дедлайн drain-poll (~2×max(idle=30s, sweep=10s)) |
| `LOADGEN_RECONNECT_RATE` | `0` | Доля клиентов с периодическим abrupt close → resume |
| `LOADGEN_RECONNECT_EVERY` | `60s` | Период разрыва у выбранных клиентов (первый — со случайным джиттером) |
| `LOADGEN_REPORT` | `./report.json` | Путь report.json (оркестратор ставит `.scratch/loadtest/`) |
| `LOADGEN_LOG_EVERY` | `10s` | Период live-прогресса на stdout |
| `LOADGEN_NETEM_SHARE` | пусто | `clean:30,bad:50,ugly:20` — доли клиентов по netem-профилям; пусто = без SO_MARK (host-режим) |
| `LOADGEN_NETEM_BAD` | `loss 5% delay 150ms 50ms` | Строка аргументов `tc netem` для band bad (читает entrypoint контейнера) |
| `LOADGEN_NETEM_UGLY` | `loss 20% delay 300ms 150ms reorder 10%` | То же для ugly |
| `LOADGEN_NETEM_DEV` | `eth0` | Исходящий интерфейс для tc в контейнере |

## Netem (inc 2, контейнер)

Механизм: клиенту при спавне weighted pick из `NETEM_SHARE` → профиль → `SO_MARK` на dial-сокете (`netem.go`: clean=1, bad=2, ugly=3); tc в entrypoint контейнера — `prio` qdisc, `fw`-фильтры `handle N → flowid 1:N`, netem-ребёнок на band bad/ugly (немаркированный трафик → 1:1 clean). Control-plane HTTP маркируется тем же pick на каждый новый conn Transport'а. `SO_MARK` требует `cap_add: NET_ADMIN` — только контейнерный режим; на хосте share не задавать (dial упадёт EPERM → `dial_errors`). Проверка долей: `report.netem_clients` vs `report.config.netem_share`, контракт ±5%.

```bash
# docker-режим оркестратора: build --network=host (bridge DNS мёртв),
# сервер на хосте, контейнер — через host.docker.internal, report → $OUT_DIR.
LOADTEST_DOCKER=1 LOADGEN_ROOMS=30 LOADGEN_ROOM_MIX="4/4:100" \
LOADGEN_NETEM_SHARE="clean:30,bad:50,ugly:20" LOADGEN_DURATION=30s \
./scripts/loadtest-run.sh
```

Ручной прогон: `docker compose -f deploy/loadgen/docker-compose.loadgen.yml run --rm loadgen` (образ собирается только `docker build --network=host` — compose build host-net не умеет).

## Отчёт

`report.json`: join/dial latency p50/p95/p99, frames in/out, счётчики по opcode (reject 0x40–0x7F, in-room 0x60–0x6F, уведомления 0x80–0x82), reconnect attempts/success, `netem_clients` — фактические доли профилей, `drain_complete` + `rooms_dump` при недожатии, `server_metrics` — снимки exposition до/после (мерджит оркестратор). Без pass/fail-вердикта.

## Smoke

```bash
LOADGEN_ROOMS=4 LOADGEN_ROOM_MIX="4/4:100" LOADGEN_POLICY_MIX="state:50,auth:50" \
LOADGEN_TRANSPORT_MIX="ws:100" LOADGEN_DURATION=30s LOADGEN_RECONNECT_RATE=0.2 \
./scripts/loadtest-run.sh
```

## Inc 3 (план)

ENet-клиент (`client_enet.go` под `-tags enet`, dial по `enet_port` из ticket, распределение по `ENET_LISTEN_PORTS` round-robin) — следующий инкремент P27. Образ `deploy/loadgen` уже собирает бинарь с `-tags enet` — код inc3 подхватится без правок Dockerfile.
