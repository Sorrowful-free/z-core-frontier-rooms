# Тесты

Все тесты проекта лежат здесь, а не рядом с исходниками в `internal/` и `cmd/`.

Структура зеркалит слои приложения:

```text
tests/
  mocks/                — mockgen из internal/port (gomock), go generate ./tests/mocks/...
  delivery/errors/      — join_reject, in_room, wire helpers
  adapter/admission/    — ticket Issue/Validate
  adapter/reservation/  — слоты: Reserve / Admit / Revoke
  adapter/realtime/     — Room Join, policy без deadlock
  adapter/registry/     — RoomRegistry, Shutdown
  usecase/room/         — JoinRoom с gomock (Admission, Registry, …)
  delivery/http/        — (планируется) Fiber после REST
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

## Уже покрыто (80/20, первый срез)

| Пакет | Что даёт |
|-------|----------|
| `tests/delivery/errors` | `JoinRejectOpCode`, `InRoomErrorOpCode`, диапазоны `Is*OpCode` |
| `tests/adapter/admission` | Round-trip ticket, invalid/expired token, invalid credentials |
| `tests/adapter/realtime` | `OnJoin` вызывает `room.Send` без deadlock; откат map при `ErrJoinDenied` |
| `tests/adapter/registry` | `GetRoom` → `errors.Is(ErrRoomNotFound)`; `Shutdown` |
| `tests/adapter/reservation` | lifecycle слотов, expiry sweep, идемпотентный `Revoke` (peer) |
| `tests/usecase/room` | `JoinRoom`, `Create`, `Delete`, `GetList`, `IssueTicket` (gomock) |

## Моки (mockgen)

Инструмент: [go.uber.org/mock](https://github.com/uber-go/mock) (`mockgen` + `gomock`). В `go.mod`: `tool go.uber.org/mock/mockgen`, `require go.uber.org/mock`.

```bash
go generate ./tests/mocks/...
```

См. [tests/mocks/README.md](mocks/README.md).

## Следующий приоритет (оставшиеся 20% усилий → много пользы)

1. **Интеграция reservation** — `RegisterRoom` в `CreateRoom`, `Reserve`/`Admit`/`Revoke` в Issue/Join/Leave (порт готов, wiring в use case — в работе).
2. **`usecase/room`** — `LeaveRoom` + reservation, ошибки `Replace`, `Start` failure.
3. **`adapter/realtime` `Send`** — broadcast с `ExcludePeerID` не доставляет отправителю (stub peers + счётчик `Deliver`).
4. **`Peer.Stop`** — не зависает: mock `Connection` с блокирующим `Receive`, `Stop` завершается после `Close`.
5. **`delivery/ws`** — `app.Test`: невалидный token → ответный opcode в записи conn (mock transport) — после стабилизации тестового harness.
6. **Интеграция** — create room → issue ticket → join (опционально, дороже в поддержке).

При добавлении join-ошибки — кейс в `join_reject_test.go` (см. [.cursor/rules/join-error-opcodes.mdc](../.cursor/rules/join-error-opcodes.mdc)).

Подробнее о слоях, OpCode и use case — [README.md](../README.md) в корне репозитория.
