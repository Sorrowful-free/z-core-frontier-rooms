# internal/loadgen

Нагрузочный клиент флота комнат (план [P27](../../docs/plans/P27-loadgen.md), inc 1–3: host/WS + Docker/netem + ENet-клиент). Бинарь — `cmd/loadgen`, оркестратор — `scripts/loadtest-run.sh` (поднимает `cmd/rooms` с пресетом [.env.loadtest](../../.env.loadtest), снимает `GET /metrics` до/после в `report.server_metrics`).

## Поток

`fleet.Run(ctx)`: ramp (`POST /rooms` → `POST /rooms/:id/tickets` × occupancy-1 → dial всех клиентов) → sustain до `LOADGEN_DURATION` → drain (abrupt close всех conn → poll `rooms_active_rooms` + `rooms_reservation_active_slots` до нуля с дедлайном) → `report.json`. Комнаты не удаляются — drain и есть проверка «сервер не оставляет пустых комнат». SIGINT → partial-отчёт (`interrupted: true`).

## Peer

`DataConn` (`Send`/`Recv`/`Close`) — транспорт-агностичный кадр OpCode+payload; реализации — WS (`fasthttp/websocket`) и ENet (`client_enet.go`, `-tags enet`). Обязательный read-loop: сервер отвечает ping только при читающем клиенте — мёртвый reader = idle-eviction. Join = первый кадр `0x01`; до него однобайтовый `0x40–0x7F` — reject. Клиент держит реплику `RoomState` (codec) — peerID по nick'у, `is_master` → send entities. mode=state: `PatchInput` всем (`INPUT_HZ`), `PatchEntities` master'у (`ENTITIES_HZ`); occupancy — только жить. Resume: `?token=<reconnect_token>` после abrupt close (клиенты из `RECONNECT_RATE`, период `RECONNECT_EVERY`).

## Env (LOADGEN_*)

| Переменная | Default | Описание |
|------------|---------|----------|
| `LOADGEN_HTTP_ADDR` | `127.0.0.1:3000` | REST сервера (host:port, без схемы) |
| `LOADGEN_WS_ADDR` | `=HTTP_ADDR` | WS endpoint (`/ws?token=`) |
| `LOADGEN_ENET_ADDR` | host `WS_ADDR` | host ENet listeners (без порта — порт из ticket `enet_port`) |
| `LOADGEN_ADMIN_ADDR` | `127.0.0.1:9091` | admin listener `GET /metrics` — drain-poll |
| `LOADGEN_ROOMS` | `1000` | Целевое число комнат |
| `LOADGEN_ROOM_MIX` | `4/4:70,8/6:20,16/4:10` | `capacity/occupancy:вес`, occupancy ≤ capacity ≤ 127 |
| `LOADGEN_TRANSPORT_MIX` | `ws:100` | Доли транспортов `ws`/`enet`; `enet` требует сборку с `-tags enet` (иначе → `dial_errors{transport=enet}`) |
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
LOADGEN_ROOMS=4 LOADGEN_ROOM_MIX="4/4:100" \
LOADGEN_TRANSPORT_MIX="ws:100" LOADGEN_DURATION=30s LOADGEN_RECONNECT_RATE=0.2 \
./scripts/loadtest-run.sh
```

## ENet (inc 3, `-tags enet`)

`enetDataConn` (`client_enet.go`, `//go:build enet && cgo`) — клиентский libenet host под `adapterenet.HostLoop` (все libenet-вызовы через `Call`/`Do`, как у серверной стороны); без тега — `client_enet_stub.go` отдаёт `ErrEnetUnsupported` на dial. Dial: `Connect(LOADGEN_ENET_ADDR:enet_port)` — порт **строго** из ticket-ответа (`enet_port`, strict binding P18 — распределение по `ENET_LISTEN_PORTS` делает сервер, `enet_port=0` → dial error); pump ждёт `EventConnect` в пределах dial-таймаута peer'а, затем шлёт admit-пакет — **сырой decoded ticket** на channel 0 reliable (WS отдаёт base64url в query, на ENet декодирует клиент). Кадры — `[op|payload]` channel 0 reliable в обе стороны. Resume reconnect-токеном работает тем же путём (dial → admit).

Известное ограничение: **SO_MARK/netem к ENet не применяется** — go-enet host владеет UDP-сокетом и не отдаёт fd; enet-трафик уходит немаркированным → в контейнере попадает в clean-band tc (в `netem_clients` enet-пиры при этом считаются по назначенному профилю).

Docker smoke (образ `zcore-loadgen` собирает `/app/loadgen` с `-tags enet`; сервер в сети по alias `rooms`, порты `ENET_LISTEN_PORTS` из пресета):

```bash
docker build --network=host -t zcore-loadgen -f deploy/loadgen/Dockerfile .
LOADTEST_DOCKER=1 LOADGEN_ENET_ADDR=rooms LOADGEN_ROOMS=10 \
LOADGEN_ROOM_MIX="4/4:100" \
LOADGEN_TRANSPORT_MIX="ws:70,enet:30" LOADGEN_DURATION=15s \
./scripts/loadtest-run.sh
# ожидание: join_success ≈ spawned, dial_errors{transport=enet} = 0, drain_complete=true
```
