# Тесты

Все тесты проекта лежат здесь, а не рядом с исходниками в `internal/` и `cmd/`.

Структура зеркалит слои приложения:

```text
tests/
  mocks/                — mockgen из internal/port (gomock), go generate ./tests/mocks/...
  delivery/errors/      — join_reject, reservation_reject, in_room, wire helpers
  adapter/config/       — LoadFromEnv, admission + http auth + transport + enet env
  adapter/httpauth/     — HTTPAuthConfig.Validate
  adapter/httplimits/   — HTTPLimitsConfig, Limiter
  adapter/admission/    — ticket Issue/Validate, AdmissionConfig
  adapter/reservation/  — слоты: Reserve / Admit / Revoke / State
  adapter/identity/     — Counter: AllocateRoomID / AllocatePeerID, wrap skip 0
  adapter/realtime/     — Room Join/Replace, policy без deadlock, peer factory nick
  adapter/realtime/policy/state/ — StateRoomPolicy, master election, tick intervals
  adapter/realtime/codec/ — round-trip state/input/rpc/room wire codec
  domain/               — ValidateNickName, DecodeIncomingFrame
  domain/state/         — atomic apply patch (entity/component)
  adapter/registry/     — RoomRegistry, Shutdown
  adapter/metrics/      — prometheus Metrics: counters/gauges через Gatherer, MetricsConfig.Validate
  delivery/admin/       — GET /metrics admin listener'а: 200 + exposition-текст (app.Test)
  usecase/room/         — Create, Delete, GetList, IssueTicket, JoinRoom, LeaveRoom (gomock)
  testutil/peer/        — параметризуемый realtime.Peer: ping/alive/deliverErr + захват PeerEvent
  testutil/env/         — общий composition root: adapters + use cases + HTTP/WS routes на fiber.App
                          (defaults обязаны зеркалить prod transport config: zero-значения маскируют ошибки доставки — см. кейс ErrQueueFull)
  delivery/http/        — REST JSON (app.Test поверх testutil/env)
  delivery/ws/          — WS join lifecycle (реальный listener + fasthttp/websocket)
  integration/          — сквозные сценарии: room lifecycle create→issue→2×WS join→disconnect→leave;
                          e2e live match (-short skip): общий драйвер + клиенты WS и ENet —
                          WS (~60s): 4 клиента, host dropout → idle evict → 2 миграции master;
                          ENet (~115s, tag enet): то же, dropout = молчание libenet → peer timeout;
                          mixed (~115s, tag enet): host на ENet, slaves WS+ENet, кросс-транспорт
```

Пакеты — **внешние** (`package *_test`), импорт модуля `github.com/Sorrowful-free/z-core-frontier-rooms/...`.

Запуск из корня модуля:

```bash
go test ./tests/...
```

С race detector (рекомендуется для realtime):

```bash
go test -race ./tests/...
```

ENet (cgo, tag `enet`) — smoke компиляции `go-enet` пакетов:

```bash
# Linux/macOS / MSYS2: нужны gcc и CGO_ENABLED=1
CGO_ENABLED=1 go test -tags enet ./tests/delivery/enet/...
```

Полный прогон тестов с тем же тегом (пересборка зависимостей под `enet`):

```bash
CGO_ENABLED=1 go test -tags enet ./tests/...
```

Windows (PowerShell, MSYS2 gcc не в PATH сессии):

```powershell
$env:Path = "C:\msys64\mingw64\bin;" + $env:Path
$env:CGO_ENABLED = "1"
go test -tags enet ./tests/delivery/enet/...
go test -tags enet ./tests/...
```

Без `-tags enet` в `tests/delivery/enet` — один skip-тест с подсказкой.

## Бенчмарки

Файлы `*_bench_test.go` — перформанс-тесты hot path (codec, patch, policy tick, room loop). Production-код не меняется.

Запуск всех бенчмарков:

```bash
go test -bench=Benchmark -benchmem -count=1 ./tests/...
```

Выборочно:

```bash
go test -bench=BenchmarkDecodeIncomingFrame -benchmem -count=1 ./tests/domain/...
go test -bench=BenchmarkRoomState_ -benchmem -count=1 ./tests/domain/state/...
go test -bench=BenchmarkRoomStateCodec_ -benchmem -count=1 ./tests/adapter/realtime/codec/...
go test -bench=BenchmarkOnTickPatchState -benchmem -count=1 ./tests/adapter/realtime/policy/state/...
go test -bench=BenchmarkRoom_ -benchmem -count=1 ./tests/adapter/realtime/...
go test -bench=BenchmarkReservation -benchmem -count=1 ./tests/adapter/reservation/...
```

Сравнение до/после (локально): `go install golang.org/x/perf/cmd/benchstat@latest`, затем `-count=10` и `benchstat`.

Reservation (per-room lock vs legacy global mutex в одном прогоне):

```bash
go test -bench=BenchmarkReservation_Reserve_ParallelSpread -benchmem -count=10 ./tests/adapter/reservation/... > bench.txt
go install golang.org/x/perf/cmd/benchstat@latest
benchstat bench.txt
```

Смотреть sub-bench `per_room_lock` vs `global_legacy` при `rooms=32` / `rooms=128`.

### Tiers

| Tier | Пакет | Что измеряется |
|------|-------|----------------|
| A — CPU | `tests/domain`, `tests/domain/state`, `tests/adapter/realtime/codec` | `DecodeIncomingFrame`, `MakePatch`/`ApplyPatch`/`Clone`, wire encode/decode |
| B — policy | `tests/adapter/realtime/policy/state` | `OnTickPatchState`, `OnMessage` PatchInput (без сети) |
| C — room | `tests/adapter/realtime` | `Room.Send` broadcast, `Deliver`, `Join` |
| D — reservation | `tests/adapter/reservation` | `Reserve` / issue-join path; parallel spread `per_room_lock` vs `global_legacy` |

Фикстуры комнаты с N peers: [`tests/testutil/bench/room_state.go`](testutil/bench/room_state.go) (`BuildRoomState`).

**Не в скоупе:** CI regression (benchstat baseline), нагрузочные soak-тесты, ENet/WS e2e perf.

### Файлы бенчмарков

| Шаг | Файл | Benchmark |
|-----|------|-----------|
| bench-01 | `tests/domain/frame_wire_bench_test.go` | `BenchmarkDecodeIncomingFrame` |
| bench-02…03 | `tests/domain/state/room_state_bench_test.go` | `BenchmarkRoomState_MakePatch` (0/8/64), `ApplyPatch`, `Clone` (8/64) |
| bench-04…05 | `tests/adapter/realtime/codec/room_state_codec_bench_test.go` | `BenchmarkRoomStateCodec_*` (peers 0/8/64; patch ping + add_entity) |
| bench-06 | `tests/adapter/realtime/codec/input_state_codec_bench_test.go` | `BenchmarkInputStateCodec_EncodePatch/DecodePatch` |
| bench-06b | `tests/adapter/realtime/codec/rpc_state_codec_bench_test.go` | `BenchmarkRpcStateCodec_Encode/Decode` |
| bench-06c | `tests/adapter/realtime/codec/entities_state_codec_bench_test.go` | `BenchmarkEntitiesStateCodec_Encode/Decode/EncodePatch` |
| bench-07 | `tests/adapter/realtime/policy/state/harness_bench_test.go` | harness для bench |
| bench-08…09 | `tests/adapter/realtime/policy/state/tick_bench_test.go` | `BenchmarkOnTickPatchState`, `NoChanges` (8/64 peers) |
| bench-10 | `tests/adapter/realtime/policy/state/message_bench_test.go` | `BenchmarkOnMessage_PatchInput` |
| bench-10b | `tests/adapter/realtime/policy/state/message_bench_test.go` | `BenchmarkOnMessage_PatchEntities` (`add_entity` / `update_value`) |
| bench-11 | `tests/adapter/realtime/room_send_bench_test.go` | `BenchmarkRoom_Send_Broadcast` (8/32/64 peers) |
| bench-12 | `tests/adapter/realtime/room_deliver_bench_test.go` | `BenchmarkRoom_Deliver` (`queue_empty` / `queue_full`) |
| bench-12b | см. bench-12 | sub-bench `queue_full` |
| bench-13 | `tests/adapter/realtime/room_join_bench_test.go` | `BenchmarkRoom_Join` |
| bench-14 | `tests/adapter/reservation/reservation_bench_test.go` | `BenchmarkReservation_Reserve`, `IssueJoinPath`, `Reserve_ParallelSpread`, `Reserve_ParallelSameRoom` |

## Фаззинг

`tests/adapter/realtime/codec/fuzz_test.go` — фаззинг всех decoder'ов wire codec. Инварианты: decode не паникует, повторный decode тех же байт детерминирован, `Encode` на декодированном значении не падает (byte-exact roundtrip не проверяется — поля переменной длины).

`-fuzz` матчит ровно один `Fuzz*` target за прогон:

```bash
go test -fuzz=FuzzRoomStateDecode -fuzztime=30s ./tests/adapter/realtime/codec/
```

Цели: `FuzzRoomStateDecode`, `FuzzRoomStateDecodePatch`, `FuzzEntitiesStateDecode`, `FuzzEntitiesStateDecodePatch`, `FuzzInputStateDecode`, `FuzzInputStateDecodePatch`, `FuzzRpcStateDecode`.

В CI — job `fuzz` в `.github/workflows/test.yml` (30s на цель); крашеры сохраняются артефактом из `tests/adapter/realtime/codec/testdata/fuzz/`.

## Покрытие по пакетам

| Пакет | Сценарии |
|-------|----------|
| `tests/delivery/errors` | `JoinRejectOpCode`, `ReservationRejectOpCode`, делегирование reservation в join, `InRoomErrorOpCode`, диапазоны `Is*OpCode` |
| `tests/adapter/config` | `LoadFromEnv`: admission + HTTP API key / disabled; transport idle envs (`TRANSPORT_PEER_IDLE_TIMEOUT`, `TRANSPORT_PEER_IDLE_SWEEP_INTERVAL`) и enet envs (`ENET_LISTEN_PORTS` CSV / `ENET_LISTEN_PORT` alias + precedence, `ENET_PEER_LIMIT`, `ENET_ADMIT_TIMEOUT`) - defaults, custom, invalid, negative, дубли портов |
| `tests/adapter/httpauth` | `HTTPAuthConfig.Validate` |
| `tests/adapter/httplimits` | `HTTPLimitsConfig.Validate`, `Limits` rate + max rooms |
| `tests/adapter/admission` | `AdmissionConfig.Validate`; round-trip ticket v3 (+ hostID в claims) + NickName, legacy v2 → `HostIDAny`, invalid nick, invalid/expired token, invalid credentials |
| `tests/adapter/realtime` | `OnJoin` → `room.Send` без deadlock; откат map при `ErrJoinDenied`; Replace rollback policy; `PeerFactory` nick; concurrent `Deliver`/`Join`/`Leave` + `Stop` без паники; `Deliver` → `ErrQueueFull` / stopped; `Send` с `ExcludePeerID` не доставляет отправителю; `Peer.Stop` разблокирует `Receive` через `Close`; metrics (P19b): первый `master_elections` на join, re-election на уходе мастера, двойной `Leave` не декрементит `active_peers`/`peer_leaves` повторно |
| `tests/adapter/realtime/policy/state` | join/full/patch/master; `NewStateRoomPolicy` zero intervals → defaults; `PatchEntities` add/update/reject non-master; malformed payload → `ErrInRoomInvalidPayload` + wire mapping (все 5 in-room opcodes + вложенный patch-input); send-failure проброс из `OnJoin`/tick/broadcast (`testutil/peer` SetDeliverErr); nil-master recovery (tick-перевыбор + ленивый на RPC→master) |
| `tests/domain` | `ValidateNickName` |
| `tests/domain/state` | atomic `ApplyPatch` rollback на entity/component |
| `tests/adapter/realtime/codec` | Round-trip `InputStateCodec`, `RpcStateCodec`, `RoomStateCodec` (full + patch); fuzz всех Decode/DecodePatch |
| `tests/adapter/registry` | `GetRoom` → `ErrRoomNotFound`; `Shutdown`; `DeleteRoom` при concurrent `Deliver` без паники; `CreateRoom` ×2 с одним id — один победитель, проигравшая комната остановлена; `GetList` не блокируется медленным `Stop` |
| `tests/adapter/reservation` | Reserve → Admit → Revoke; expiry sweep; идемпотентный `Revoke`; `State` (none / reserved / admitted); concurrent ops по разным комнатам; metrics (P19b): expired-slot в `Admit` → `reservation_ops{revoke}` + `reservation_active_slots`, lazy sweep на `Reserve` → revoke по удалённым |
| `tests/adapter/metrics` | новые сигналы P19b через `Gather()`: queue-depth gauges, tick summary, `peer_send_errors`, `enet_host_peers`, host-loop latency summary, `reservation_active_slots`, `sweep_runs_total`, http histogram; `Noop`/`OrNoop` |
| `tests/adapter/identity` | первый ID = 1; независимые room/peer; ctx cancel; wrap `MaxUint32` → 1; уникальность под конкуренцией |
| `tests/usecase/room` | **Create** — success, rooms limit, ctx cancel, registry rollback + `errors.Join` на unregister; **Delete** — success, kick peers, room not found; **GetList**; **IssueTicket** — success, room/peer errors, slot held, orphan admitted cleanup, issue+revoke join; **JoinRoom** — validate error, room not found после validate, success, admit/join/revoke откаты, `errors.Join` при fail Revoke; **LeaveRoom** — success, not found, revoke idempotent; **EvictIdlePeers** — `RunOnce`: zombie peer evicted, alive skipped, per-peer error tolerated; **sweeps** — `SweepHandle.Stop` ждёт in-flight `RunOnce`, bad room пропускается (orphan sweep), nil-handle при `interval <= 0` |
| `tests/delivery/http` | API key; rate limit 429 (create/issue/delete/list); rooms limit 503; create → list → delete; issue ticket; 409/404/400; `invalid_capacity` (0 и >127) |
| `tests/delivery/ws` | невалидный token → `OpInvalidToken` frame + close; missing token → `OpEmptyToken`; malformed base64 → `OpInvalidToken`; issue ticket → join → peer в `Registry` → disconnect → leave |
| `tests/adapter/transport/enet` | `EnetConfig.Validate` (defaults + граничные значения) |
| `tests/delivery/admin` | `GET /metrics` happy path; падающий `Gatherer` → 500 с контекстом (P19b) |
| `tests/delivery/enet` | stub-Listen чистый выход (без тега); e2e под `-tags enet` + cgo: bind-failure → `Err`, `Stop` ждёт loop, admit-timeout disconnect, join-reject доезжает (`DisconnectLater`) |
| `tests/integration` | room lifecycle: create (host) → issue (joiner) → оба join по WS → 2 peers → joiner disconnect → 1 peer (host); e2e live match (`runLiveMatch` поверх `matchClient`): silent host → evict → master→slave, clean leave master-2 → master-3, slaves видят entities от 2+ мастеров и input друг друга; транспорты: WS (`TestE2E_LiveMatch_HostMigration`, ~60s, idle sweep evict), 4×ENet (`TestE2E_LiveMatch_ENet`, ~115s, libenet peer timeout ~10s, bound 70s), mixed ENet host + WS/ENet slaves (`TestE2E_LiveMatch_Mixed`, ~115s) — оба под `-tags enet` |

После изменения `internal/port/*` интерфейсов:

```bash
go generate ./tests/mocks/...
```

См. [tests/mocks/README.md](mocks/README.md).

## E2E live match

Общий клиентский слой и сценарный драйвер — `match_e2e_client_test.go` (`matchClientBase` + `matchClient` + `runLiveMatch`); транспорты: `wsClient` (`match_e2e_test.go`) и `enetClient` (`match_e2e_enet_test.go`, tag `enet && cgo`). Все сценарии пропускаются под `-short`.

`TestE2E_LiveMatch_HostMigration` (план `docs/plans/P11-e2e-match.md`) — 4 WS-клиента, ~60s:

```bash
go test -v -race -run '^TestE2E_LiveMatch_HostMigration$' -count=1 -timeout 120s ./tests/integration/
```

`TestE2E_LiveMatch_ENet` — 4 ENet-клиента, ~115s: dropout host'а — честное молчание libenet (клиент перестаёт вызывать `host.Service`, UDP-сокет жив) → серверский peer timeout (измерено ~10s на localhost, bound 70s) → `EventDisconnect` → `LeaveRoom` → миграция. `TestE2E_LiveMatch_Mixed` — host на ENet, slaves 1×ENet + 2×WS: кросс-транспорт entities/input/миграции. Локально без libenet-dev — через Docker:

```bash
./scripts/enet-verify.sh test   # delivery/enet + tagged e2e в образе zcore-rooms-dev
# или при установленном libenet-dev:
CGO_ENABLED=1 go test -v -race -tags enet -run 'TestE2E' -count=1 -timeout 300s ./tests/integration/
```

Каждый клиент (`host`, `s1..s3`/`e1..e3`/`w1..w2`) — goroutine'ы pump + send-loop + stats-loop; логи `t.Logf` с префиксом ника образуют единую хронологическую ленту при `-v` и при падении: `joined as peer N`, `master is now ...`, `became/lost master`, `first entity snapshot from owner ...`, `observed input of ...`, `my input observed`, `in-room error frame`, `disconnected`. Маркеры сценария — `[scenario] ...` (host goes silent, master leaves, шаги `waitFor` — ok).

## Следующий приоритет

Очередь пуста — первый интеграционный сценарий (`tests/integration`, ADR-0002) закрыт.

При новой join/reservation-ошибке — кейс в `join_reject_test.go` / `reservation_reject_test.go` (см. [.cursor/rules/join-error-opcodes.mdc](../.cursor/rules/join-error-opcodes.mdc)).

Подробнее — индекс [README.md](../README.md), архитектура [docs/architecture.md](../docs/architecture.md), пакеты `internal/**/README.md`.
