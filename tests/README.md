# Тесты

Все тесты проекта лежат здесь, а не рядом с исходниками в `internal/` и `cmd/`.

Структура зеркалит слои приложения:

```text
tests/
  mocks/                — mockgen из internal/port (gomock), go generate ./tests/mocks/...
  delivery/errors/      — join_reject, reservation_reject, in_room, wire helpers
  adapter/config/       — LoadFromEnv, admission + http auth env
  adapter/httpauth/     — HTTPAuthConfig.Validate
  adapter/httplimits/   — HTTPLimitsConfig, Limiter
  adapter/admission/    — ticket Issue/Validate, AdmissionConfig
  adapter/reservation/  — слоты: Reserve / Admit / Revoke / State
  adapter/identity/     — Counter: AllocateRoomID / AllocatePeerID, wrap skip 0
  adapter/realtime/     — Room Join/Replace, policy без deadlock, peer factory nick
  adapter/realtime/policy/state/ — StateRoomPolicy, master election, tick intervals
  adapter/realtime/codec/ — round-trip state/input/rpc/room wire codec
  domain/               — ValidateNickName
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

## Покрытие по пакетам

| Пакет | Сценарии |
|-------|----------|
| `tests/delivery/errors` | `JoinRejectOpCode`, `ReservationRejectOpCode`, делегирование reservation в join, `InRoomErrorOpCode`, диапазоны `Is*OpCode` |
| `tests/adapter/config` | `LoadFromEnv`: admission + HTTP API key / disabled |
| `tests/adapter/httpauth` | `HTTPAuthConfig.Validate` |
| `tests/adapter/httplimits` | `HTTPLimitsConfig.Validate`, `Limits` rate + max rooms |
| `tests/adapter/admission` | `AdmissionConfig.Validate`; round-trip ticket + NickName, invalid nick, invalid/expired token, invalid credentials |
| `tests/adapter/realtime` | `OnJoin` → `room.Send` без deadlock; откат map при `ErrJoinDenied`; Replace rollback policy; `PeerFactory` nick |
| `tests/adapter/realtime/policy/state` | join/full/patch/master; `NewStateRoomPolicy` zero intervals → defaults |
| `tests/domain` | `ValidateNickName` |
| `tests/domain/state` | atomic `ApplyPatch` rollback на entity/component |
| `tests/adapter/realtime/codec` | Round-trip `InputStateCodec`, `RpcStateCodec`, `RoomStateCodec` (full + patch) |
| `tests/adapter/registry` | `GetRoom` → `ErrRoomNotFound`; `Shutdown` |
| `tests/adapter/reservation` | Reserve → Admit → Revoke; expiry sweep; идемпотентный `Revoke`; `State` (none / reserved / admitted) |
| `tests/adapter/identity` | первый ID = 1; независимые room/peer; ctx cancel; wrap `MaxUint32` → 1; уникальность под конкуренцией |
| `tests/usecase/room` | **Create** — success, rooms limit, ctx cancel, registry rollback + `errors.Join` на unregister; **Delete** — success, kick peers, room not found; **GetList**; **IssueTicket** — success, room/peer errors, slot held, orphan admitted cleanup, issue+revoke join; **JoinRoom** — validate error, success, admit/join/revoke откаты; **LeaveRoom** — success, not found, revoke idempotent |
| `tests/delivery/http` | API key; rate limit 429; rooms limit 503; create → list → delete; issue ticket; 409/404/400 |

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
