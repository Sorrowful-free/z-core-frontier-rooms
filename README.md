# z-core-frontier-rooms

Сервис игровых комнат: **control plane** по HTTP ([Fiber](https://gofiber.io/) v3) и **data plane** по WebSocket и ENet.

Документ отражает **текущее** состояние репозитория (имена use case, wiring в `cmd`, известные пробелы).

## Статус

| Область | Состояние |
|--------|-----------|
| `domain/`, `port/` | Типы, события, интерфейсы без зависимостей от инфраструктуры |
| `adapter/realtime` | `Room`, `Peer`, фабрики; `RelayRoomHandler` — временная заглушка relay |
| `adapter/registry` | In-memory `RoomRegistry` |
| `adapter/admission` | HMAC-SHA256 ticket, TTL (`Issue` / `Validate`) |
| `adapter/transport` | WebSocket; ENet (CGO + build tag `enet`) |
| Use case `Create` / `Delete` / `GetList` | Реализованы; HTTP handlers — **заглушки** (use case не вызываются) |
| Use case `IssueTicket` | Реализован; в `cmd` подключён; **HTTP-маршрута пока нет** |
| Use case `JoinRoom` | Data plane: validate ticket → peer → `room.Join` → `Start`; WS + ENet в `cmd`; при ошибке join — OpCode на wire (`delivery/joinerror`) |
| Use case `LeaveRoom` | `GetPeer` → `room.Leave` → `peer.Stop`; вызывается при disconnect WS/ENet |
| `delivery/http` | Маршруты REST; тела/JSON — TODO |
| `delivery/ws` | `GET /ws` (upgrade), `JoinRoom` + `LeaveRoom` |
| `delivery/enet` | Host loop в goroutine; первый пакет = ticket; без `enet`+cgo — stub с warn |
| `cmd/rooms` | Fiber `:3000`, ENet `DefaultConfig()` (порт **7777**), zap middleware |
| Конфиг (env), graceful shutdown, health | TODO |

Сборка по умолчанию: `go build ./cmd/rooms` — HTTP + WS. ENet host **активен в runtime** только при сборке с `-tags enet` и `CGO_ENABLED=1`.

## Назначение

- Хранить **комнаты** (`Room`) и подключённых **пиров** (`Peer`).
- **Допуск:** выдача ticket (control plane, `IssueTicket`) и вход по realtime (`JoinRoom`).
- **Выход:** снятие пира с комнаты (`LeaveRoom`) при отключении транспорта.
- **Relay** кадров между пирами — через `RoomHandler` / `RelayRoomHandler` (черновик).
- Единый контракт `transport.Connection` для WS и ENet; протокол — только в `delivery`.

## Терминология (use case)

| Use case | Плоскость | Смысл |
|----------|-----------|--------|
| **`IssueTicket`** | Control (HTTP, планируется) | `admission.Issue` → бинарный ticket |
| **`JoinRoom`** | Data (WS / ENet) | `Validate` → peer → membership → `peer.Start()` |
| **`LeaveRoom`** | Data (при disconnect) | `room.Leave` + `peer.Stop()` |

Слово *connect* в коде относится к **транспорту** (`transport.Connection`, upgrade WS, ENet `EventConnect`), а не к отдельному use case.

## Архитектура (слои)

```text
cmd/rooms                 — composition root: DI, Fiber, ENet Listen()

delivery/http             — REST control plane
delivery/ws               — WebSocket → JoinRoom / LeaveRoom
delivery/enet             — ENet host loop → JoinRoom / LeaveRoom

usecase/room/             — Create, Delete, GetList, IssueTicket, JoinRoom, LeaveRoom

port/                     — admission, registry, realtime, transport, logging
domain/                   — типы и события
adapter/                  — admission, realtime, registry, transport, logging
```

Зависимости: `delivery` → `usecase` → `port` ← `adapter`; `domain` ни от кого не зависит.

### Composition root (`cmd/rooms`)

```text
fiber.New() + zap middleware
  → roomFactory, relayRoomHandlerFactory, roomRegistry, peerFactory
  → admission (dev secret в коде, TTL 1h)
  → Create, IssueTicket, JoinRoom, LeaveRoom, Delete, GetList
  → http.RoomsHandler.RegisterRoutes(app)
  → ws.RoomsHandler.RegisterRoutes(app)
  → enet.RoomsHandler.Listen()   // goroutine, отдельный порт
  → app.Listen(":3000")
```

WebSocket — на **том же** `*fiber.App` (HTTP upgrade). ENet — **отдельный** UDP host, не Fiber.

### Поток выдачи ticket (control plane)

`internal/usecase/room/issue_ticket.go` — `IssueTicket` → `admission.Issue`.

Клиент получает ticket (пока только программно / будущий HTTP). Затем подключается по WS или ENet.

**Пробел:** `IssueTicketUseCase` есть в DI HTTP handler, но маршрут и вызов `IssueTicket` в `delivery/http` **не реализованы**. Метод `JoinRoom` в HTTP — заглушка без регистрации в `RegisterRoutes`.

### Поток входа в комнату (data plane)

`internal/usecase/room/join_room.go` — `JoinRoom(ctx, connection, token)`.

```text
1. admission.Validate(ctx, token) → domain.Claims
2. roomRegistry.GetRoom(claims.RoomID)
3. peerFactory.CreatePeer(claims.PeerID, connection, room, logger)
4. room.Replace(peer) если peer уже в комнате, иначе room.Join(peer)
5. peer.Start()
```

При ошибке на шаге 5: `room.Leave(peer)`, `peer.Stop()`. При любой ошибке join delivery шлёт **один binary-кадр** с OpCode ошибки (`joinerror.Send`), затем закрывает transport (`defer Close` на WS, `sess.close` на ENet). До `peer.Start()` use case **не** вызывает `peer.Stop()` — только откат membership в `Room`.

Ошибки join оборачивают sentinel из `domain/join_errors.go` (`%w`); маппинг в OpCode — **только** в `internal/delivery/joinerror` (чеклист — [.cursor/rules/join-error-opcodes.mdc](.cursor/rules/join-error-opcodes.mdc)).

Возвращает `(RoomSummary, PeerID, error)` — `PeerID` нужен для `LeaveRoom` при disconnect.

| Слой | Роль |
|------|------|
| `delivery/ws`, `delivery/enet` | Транспорт, извлечение token, `JoinRoom`, при отключении — `LeaveRoom` |
| `JoinRoomUseCase` | validate → комната → peer → join/replace → start |
| `LeaveRoomUseCase` | get peer → leave → stop |
| `Admission` | `Issue` / `Validate`, без знания WS/ENet |
| `adapter/realtime` | `Room`, `Peer`, `RoomHandler` |

### Поток выхода (data plane)

`internal/usecase/room/leave_room.go` — `LeaveRoom(ctx, roomID, peerID)`.

- **WebSocket:** после `connection.Wait()` (клиент отключился) → `LeaveRoom`.
- **ENet:** `EventDisconnect`, если сессия была admitted → `LeaveRoom`, затем `sess.close()`.

### Допуск (Admission)

Порт: `internal/port/admission/admission.go`.  
Реализация: `internal/adapter/admission` (HMAC-SHA256, фиксированный размер token 33 байта).

- **`Issue`** — `roomID`, `peerID`, `password` → `[]byte` ticket.
- **`Validate`** — `token` → `domain.Claims` или ошибка (`domain.ErrInvalidToken`, `domain.ErrExpiredToken`, …).

`domain.Claims`: `RoomID`, `PeerID`, `IssuedAt`, `ExpiresAt`.

В `main` секрет зашит как `dev-secret-change-me` (только для разработки).

### Realtime

- **`Room`** — lifecycle, `Join` / `Leave` / `Replace`, `GetPeer`, `Deliver` / `Send`.
- **`Peer`** — чтение/запись кадров через `transport.Connection`.
- **`RoomHandler`** — хуки; **`RelayRoomHandler`** — временный relay между пирами.

### Реестр комнат

- Порт: `internal/port/registry/room_registry.go`
- Реализация: `internal/adapter/registry/room_registry.go` (in-memory `map[RoomID]Room`)

### Транспорт

`internal/port/transport/connection.go` — `Send`, `Receive`, `Close` над `domain.Frame`.

| Адаптер | Пакет |
|---------|--------|
| WebSocket | `internal/adapter/transport/ws/` |
| ENet | `internal/adapter/transport/enet/` (`connection_cgo.go` / stub) |

### Wire: OpCode (data plane)

Кадр: **1 байт `OpCode` + `Payload`**. Для ошибок join **payload пустой**; клиент мапит `OpCode` → свои UI-строки.

| OpCode | Hex | Смысл (join / admit) |
|--------|-----|----------------------|
| `OpEmptyToken` | `0x40` | Пустой ticket (ENet admit) |
| `OpInvalidToken` | `0x41` | Невалидный ticket |
| `OpExpiredToken` | `0x42` | Ticket истёк |
| `OpRoomNotFound` | `0x43` | Комната не найдена |
| `OpJoinDenied` | `0x44` | Handler отклонил join |
| `OpReplaceFailed` | `0x45` | Ошибка replace |
| `OpPeerNotFound` | `0x46` | Peer не в комнате (replace) |
| `OpInternal` | `0x50` | Прочая ошибка сервера при join |
| `OpPeerStartFailed` | `0x51` | Не удалось `peer.Start()` |

Зарезервировано: `0x01–0x3F` — игровой трафик / relay; `0x60–0x6F` — ошибки **внутри комнаты** (отдельная задача).

Источник констант: `internal/domain/opcodes.go`. Маппер: `internal/delivery/joinerror`.

## API и endpoints

### HTTP (Fiber), порт `:3000`

| Метод | Путь | Handler | Use case | Ответ |
|-------|------|---------|----------|--------|
| `POST` | `/rooms` | `CreateRoom` | `Create` | заглушка |
| `GET` | `/rooms` | `GetListRooms` | `GetList` | заглушка |
| `DELETE` | `/rooms/:id` | `DeleteRoom` | `Delete` | заглушка |

Планируется маршрут выдачи ticket (например `POST /rooms/:id/tickets` → `IssueTicket`). Сейчас **не зарегистрирован**.

### WebSocket

| Метод | Путь | Параметры | Поведение |
|-------|------|-----------|-----------|
| `GET` | `/ws` | `token` (query) | Upgrade → `JoinRoom` → ожидание disconnect → `LeaveRoom` |

При ошибке `JoinRoom`: один кадр с OpCode из таблицы выше → закрытие WebSocket.

**Ограничение:** ticket бинарный (33 байта); передача в query string без base64/hex **может ломать** валидацию. Для продакшена — кодирование ticket или другой канал (header / первое binary-сообщение). Отсутствие `token` в query — закрытие **без** кадра (`0x40` не отправляется).

### ENet

| Параметр | Значение по умолчанию (`DefaultConfig`) |
|----------|----------------------------------------|
| Порт | `7777` |
| Первый пакет после connect | сырой `token` (`admit`) |
| Ошибка admit | один кадр OpCode (`0x40`–`0x51`) → disconnect |
| После admit | binary-кадры (`OpCode` + payload) |

Сборка с ENet:

```bash
CGO_ENABLED=1 go build -tags enet -o bin/rooms ./cmd/rooms
```

Без тега/cgo: `Listen()` пишет warn и не поднимает host.

## Структура репозитория

```text
cmd/rooms/                      — точка входа
internal/
  domain/                       — RoomID, PeerID, Frame, Claims, events
  port/                         — admission, registry, realtime, transport, logging
  usecase/room/
    create.go, delete.go, get_list.go
    issue_ticket.go             — IssueTicketUseCase
    join_room.go                — JoinRoomUseCase
    leave_room.go               — LeaveRoomUseCase
    room_summary.go, peer_summary.go
  adapter/
    admission/                  — HMAC ticket
    realtime/                   — Room, Peer, RelayRoomHandler
    registry/
    transport/ws|enet/
    logging/stdlib|zap/
  delivery/
    http/rooms.go
    ws/rooms.go
    enet/                       — handler, config, run_cgo / run_stub
    joinerror/                  — OpCode(err), Send перед close
tests/                          — см. tests/README.md
```

## Требования

- Go 1.25+
- ENet (опционально): CGO, `github.com/codecat/go-enet`, build tag `enet`

## Сборка

```bash
go build -o bin/rooms ./cmd/rooms
```

С ENet:

```bash
CGO_ENABLED=1 go build -tags enet -o bin/rooms-enet ./cmd/rooms
```

## Запуск

```bash
go run ./cmd/rooms
```

- HTTP + WebSocket: `http://localhost:3000`
- ENet (если собран с `enet`+cgo): UDP порт **7777**

REST handlers пока не вызывают use case и не отдают JSON.

## Тесты

Тесты — в [`tests/`](tests/), не рядом с `internal/` и `cmd/`.

```bash
go test ./tests/...
go test -race ./tests/...
```

См. [tests/README.md](tests/README.md) — приоритет 80/20 и что уже покрыто.

## Соглашения проекта

- HTTP — только [Fiber](https://gofiber.io/) v3; один `*fiber.App` для REST и WS upgrade.
- Handlers в `delivery` тонкие: парсинг → use case → JSON / маппинг ошибок.
- Тесты — только под `tests/`, внешние пакеты `*_test`.
- Правила для агентов — [.cursor/rules/go-standards.mdc](.cursor/rules/go-standards.mdc), join OpCode — [.cursor/rules/join-error-opcodes.mdc](.cursor/rules/join-error-opcodes.mdc).

## TODO (ближайшее)

- HTTP: JSON request/response, вызов use case, маршрут `IssueTicket` (убрать/переименовать заглушку `JoinRoom` в http)
- Кодирование ticket для клиента (base64url) и контракт API
- Конфигурация: порт HTTP/ENet, секрет admission из env
- Graceful shutdown: отмена `ctx`, остановка Fiber и ENet
- Health-check
- Доменные ошибки и маппинг в HTTP status (control plane; data plane join — см. OpCode выше)
- Идемпотентный `LeaveRoom`, если peer уже снят с комнаты
- Замена `RelayRoomHandler` на целевую логику комнаты
- Тесты: `adapter/admission`, `JoinRoom` / `LeaveRoom`, registry
