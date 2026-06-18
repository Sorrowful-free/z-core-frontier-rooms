# Тесты

Все тесты проекта лежат здесь, а не рядом с исходниками в `internal/` и `cmd/`.

Структура зеркалит слои приложения:

```text
tests/
  mocks/                — mockgen из internal/port (gomock), go generate ./tests/mocks/...
  delivery/errors/      — join_reject, reservation_reject, in_room, wire helpers
  adapter/config/       — LoadFromEnv, admission + http auth + transport env
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
  usecase/room/         — Create, Delete, GetList, IssueTicket, JoinRoom, LeaveRoom (gomock)
  delivery/http/        — REST JSON (Fiber app.Test + in-memory adapters)
  integration/          — (планируется) сквозные сценарии
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

## Покрытие по пакетам

| Пакет | Сценарии |
|-------|----------|
| `tests/delivery/errors` | `JoinRejectOpCode`, `ReservationRejectOpCode`, делегирование reservation в join, `InRoomErrorOpCode`, диапазоны `Is*OpCode` |
| `tests/adapter/config` | `LoadFromEnv`: admission + HTTP API key / disabled |
| `tests/adapter/httpauth` | `HTTPAuthConfig.Validate` |
| `tests/adapter/httplimits` | `HTTPLimitsConfig.Validate`, `Limits` rate + max rooms |
| `tests/adapter/admission` | `AdmissionConfig.Validate`; round-trip ticket + NickName, invalid nick, invalid/expired token, invalid credentials |
| `tests/adapter/realtime` | `OnJoin` → `room.Send` без deadlock; откат map при `ErrJoinDenied`; Replace rollback policy; `PeerFactory` nick; concurrent `Deliver`/`Join`/`Leave` + `Stop` без паники; `Deliver` → `ErrQueueFull` / stopped |
| `tests/adapter/realtime/policy/state` | join/full/patch/master; `NewStateRoomPolicy` zero intervals → defaults; `PatchEntities` add/update/reject non-master |
| `tests/domain` | `ValidateNickName` |
| `tests/domain/state` | atomic `ApplyPatch` rollback на entity/component |
| `tests/adapter/realtime/codec` | Round-trip `InputStateCodec`, `RpcStateCodec`, `RoomStateCodec` (full + patch) |
| `tests/adapter/registry` | `GetRoom` → `ErrRoomNotFound`; `Shutdown`; `DeleteRoom` при concurrent `Deliver` без паники |
| `tests/adapter/reservation` | Reserve → Admit → Revoke; expiry sweep; идемпотентный `Revoke`; `State` (none / reserved / admitted); concurrent ops по разным комнатам |
| `tests/adapter/identity` | первый ID = 1; независимые room/peer; ctx cancel; wrap `MaxUint32` → 1; уникальность под конкуренцией |
| `tests/usecase/room` | **Create** — success, rooms limit, ctx cancel, registry rollback + `errors.Join` на unregister; **Delete** — success, kick peers, room not found; **GetList**; **IssueTicket** — success, room/peer errors, slot held, orphan admitted cleanup, issue+revoke join; **JoinRoom** — validate error, success, admit/join/revoke откаты; **LeaveRoom** — success, not found, revoke idempotent |
| `tests/delivery/http` | API key; rate limit 429; rooms limit 503; create → list → delete; issue ticket; 409/404/400; `invalid_capacity` (0 и >127) |
| `tests/delivery/enet` | smoke `TestEnetPackagesCompile` при `-tags enet` + cgo; без тега — skip с инструкцией |

После изменения `internal/port/*` интерфейсов:

```bash
go generate ./tests/mocks/...
```

См. [tests/mocks/README.md](mocks/README.md).

## Следующий приоритет

1. **`usecase/room`** — Join: `errors.Join` при fail Revoke после Admit; room not found после validate.
2. **`adapter/realtime` `Send`** — broadcast с `ExcludePeerID` не доставляет отправителю.
3. **`Peer.Stop`** — mock `Connection` с блокирующим `Receive`, `Stop` завершается после `Close`.
4. **`delivery/ws`** — `app.Test`: невалидный token → opcode; join с ticket из HTTP issue.
5. **Интеграция** — create → issue → join (дороже в поддержке).

При новой join/reservation-ошибке — кейс в `join_reject_test.go` / `reservation_reject_test.go` (см. [.cursor/rules/join-error-opcodes.mdc](../.cursor/rules/join-error-opcodes.mdc)).

Подробнее — индекс [README.md](../README.md), архитектура [docs/architecture.md](../docs/architecture.md), пакеты `internal/**/README.md`.
