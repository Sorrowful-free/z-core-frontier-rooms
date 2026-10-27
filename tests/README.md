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
                          reconnect (P20): WS разрыв → 0x80 → resume → 0x81 + resync;
                          ENet (tag enet): базовый reconnect, grace-expiry 0x74 (P24),
                          strict host binding 0x77;
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

### Tiers

| Tier | Пакет | Что измеряется |
|------|-------|----------------|
| A — CPU | `tests/domain`, `tests/domain/state`, `tests/adapter/realtime/codec` | `DecodeIncomingFrame`, `MakePatch`/`ApplyPatch`/`Clone`, wire encode/decode |
| B — policy | `tests/adapter/realtime/policy/state` | `OnTickPatchState`, `OnMessage` PatchInput (без сети) |
| C — room | `tests/adapter/realtime` | `Room.Send` broadcast, `Deliver`, `Join` |
| D — reservation | `tests/adapter/reservation` | `Reserve` / issue-join path; parallel spread |

Фикстуры комнаты с N peers: [`tests/testutil/bench/room_state.go`](testutil/bench/room_state.go) (`BuildRoomState`).

**Не в скоупе:** CI regression (benchstat baseline), нагрузочные soak-тесты, ENet/WS e2e perf.

## Фаззинг

`tests/adapter/realtime/codec/fuzz_test.go` — фаззинг всех decoder'ов wire codec. Инварианты: decode не паникует, повторный decode тех же байт детерминирован, `Encode` на декодированном значении не падает (byte-exact roundtrip не проверяется — поля переменной длины).

`-fuzz` матчит ровно один `Fuzz*` target за прогон:

```bash
go test -fuzz=FuzzRoomStateDecode -fuzztime=30s ./tests/adapter/realtime/codec/
```

Цели: `FuzzRoomStateDecode`, `FuzzRoomStateDecodePatch`, `FuzzEntitiesStateDecode`, `FuzzEntitiesStateDecodePatch`, `FuzzInputStateDecode`, `FuzzInputStateDecodePatch`, `FuzzRpcStateDecode`.

В CI — job `fuzz` в `.github/workflows/test.yml` (30s на цель); крашеры сохраняются артефактом из `tests/adapter/realtime/codec/testdata/fuzz/`.

## Покрытие по пакетам

Инвентарь сценариев намеренно не дублируется: он протухает при каждом
изменении тестов. Что покрывает пакет — смотрите его `*_test.go`; навигация
— по дереву каталогов выше (зеркалит слои приложения).

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
