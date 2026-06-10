# z-core-frontier-rooms

Сервис игровых комнат: **control plane** по HTTP ([Fiber](https://gofiber.io/) v3) и **data plane** по WebSocket и ENet.

Документ отражает **текущее** состояние репозитория (имена use case, wiring в `cmd`, известные пробелы).

## Статус

| Область | Состояние |
|--------|-----------|
| `domain/`, `port/` | Типы, события, интерфейсы без зависимостей от инфраструктуры |
| `adapter/realtime` | `Room` (event loop, lifecycle, ticks), `Peer`, фабрики; wire codec (`adapter/realtime/codec`); `StateRoomPolicy` (authoritative state) |
| `adapter/registry` | In-memory `RoomRegistry`, lifecycle-`ctx`, `Shutdown` |
| `adapter/reservation` | In-memory слоты: `RegisterRoom` / `Reserve` / `Admit` / `Revoke` |
| `adapter/admission` | HMAC-SHA256 ticket, TTL (`Issue` / `Validate`) |
| `adapter/transport` | WebSocket; ENet (CGO + build tag `enet`) |
| Use case `Create` / `Delete` / `GetList` | Реализованы; HTTP handlers в `delivery/http` |
| Use case `IssueTicket` | Реализован; HTTP `POST /rooms/:id/tickets` (`nick_name` обязателен) |
| Use case `JoinRoom` | Data plane: validate ticket → peer → `room.Join` → `Start`; WS + ENet в `cmd`; при ошибке join — OpCode на wire (`delivery/errors`) |
| Use case `LeaveRoom` | `GetPeer` → `room.Leave` → `peer.Stop` → `reservation.Revoke` (мягко при `ErrReservationNotFound`); disconnect WS/ENet |
| Reservation ↔ use case | **Подключено** в `cmd`: Create/Delete, IssueTicket, JoinRoom, LeaveRoom; shutdown — `GetList` + `Delete` + `registry.Shutdown` |
| `delivery/http` | REST JSON: Create, GetList, Delete, IssueTicket (`POST …/tickets`) |
| `delivery/ws` | `GET /ws` (upgrade), `JoinRoom` + `LeaveRoom` |
| `delivery/enet` | Host loop в goroutine; первый пакет = ticket; без `enet`+cgo — stub с warn |
| `cmd/rooms` | Fiber `:3000`, ENet `DefaultConfig()` (порт **7777**), zap middleware, SIGINT shutdown |
| Конфиг (env), health | TODO |

Сборка по умолчанию: `go build ./cmd/rooms` — HTTP + WS. ENet host **активен в runtime** только при сборке с `-tags enet` и `CGO_ENABLED=1`.

## Назначение

- Хранить **комнаты** (`Room`) и подключённых **пиров** (`Peer`).
- **Допуск:** выдача ticket (control plane, `IssueTicket`) и вход по realtime (`JoinRoom`).
- **Выход:** снятие пира с комнаты (`LeaveRoom`) при отключении транспорта.
- **Authoritative state** комнаты — `StateRoomPolicy`: full/patch ticks, master-only entities/input, RPC; ошибки in-room → OpCode отправителю.
- **Бронирование слотов** — `port/reservation` (отдельно от HMAC ticket); лимит мест и TTL брони до join.
- Единый контракт `transport.Connection` для WS и ENet; протокол — только в `delivery`.

## Терминология (use case)

| Use case | Плоскость | Смысл |
|----------|-----------|--------|
| **`IssueTicket`** | Control (HTTP) | `ValidateNickName` → `Reserve` → `Issue(roomID, peerID, nickName, …)`; политика A + orphan cleanup |
| **`JoinRoom`** | Data (WS / ENet) | `Validate` → `Admit` → peer → `Join`/`Replace` → `Start`; откат `Revoke` при ошибках (кроме Replace) |
| **`LeaveRoom`** | Data (при disconnect) | `Leave` → `Stop` → `Revoke` |
| **`Create` / `Delete`** | Control | `RegisterRoom`/`UnregisterRoom` ↔ registry; Create откатывает `UnregisterRoom` через `errors.Join` |

Слово *connect* в коде относится к **транспорту** (`transport.Connection`, upgrade WS, ENet `EventConnect`), а не к отдельному use case.

## Архитектура (слои)

```text
cmd/rooms                 — composition root: DI, Fiber, ENet Listen()

delivery/http             — REST control plane
delivery/ws               — WebSocket → JoinRoom / LeaveRoom
delivery/enet             — ENet host loop → JoinRoom / LeaveRoom

usecase/room/             — Create, Delete, GetList, IssueTicket, JoinRoom, LeaveRoom

port/                     — admission, registry, reservation, realtime, transport, logging
domain/                   — типы, события, reservation_errors
adapter/                  — admission, reservation, realtime, registry, transport, logging
```

Зависимости: `delivery` → `usecase` → `port` ← `adapter`; `domain` ни от кого не зависит.

### Composition root (`cmd/rooms`)

```text
fiber.New() + zap middleware
  → roomPolicyFactory, roomFactory, roomRegistry (app lifecycle ctx), peerFactory
  → admission (dev secret в коде, TTL 1h)
  → reservation (in-memory; DI во все room use case)
  → Create, IssueTicket, JoinRoom, LeaveRoom, Delete, GetList
  → http.RoomsHandler.RegisterRoutes(app)
  → ws.RoomsHandler.RegisterRoutes(app)
  → enet.RoomsHandler.Listen()   // goroutine, отдельный порт
  → app.Listen(":3000")
```

WebSocket — на **том же** `*fiber.App` (HTTP upgrade). ENet — **отдельный** UDP host, не Fiber.

### Поток выдачи ticket (control plane)

`internal/usecase/room/issue_ticket.go`:

```text
ValidateNickName (обязателен, max 64 байта UTF-8)
GetRoom → HasPeer? → ErrPeerAlreadyInRoom
Reserve(expiresAt = admission.TTL())
  при ErrTicketSlotHeld и peer не в room:
    State → если admitted (orphan) → Revoke → повторный Reserve (один раз)
Issue(roomID, peerID, nickName, password) → при ошибке Revoke (откат брони)
```

**Политика A (строгая):** peer уже в room → `ErrPeerAlreadyInRoom`; слот `reserved`/`admitted` → `ErrTicketSlotHeld`; повторный Issue при активной брони запрещён.

HTTP: `POST /rooms/:id/tickets` с `{"nick_name":"…","password":"…"}` → `{"token":"<base64url>"}`. Затем WS или ENet.

HTTP control plane реализован в `delivery/http` (см. таблицу endpoints ниже).

### Поток входа в комнату (data plane)

`internal/usecase/room/join_room.go` — `JoinRoom(ctx, connection, token)`.

```text
1. admission.Validate(ctx, token) → domain.Claims (включая NickName из ticket)
2. roomRegistry.GetRoom(claims.RoomID)
3. reservation.Admit(claims.RoomID, claims.PeerID)   // reserved → admitted
4. peerFactory.CreatePeer(room.Context(), claims.PeerID, claims.NickName, …)
5. room.Replace(peer) если peer уже в комнате, иначе room.Join(peer)
6. peer.Start()
```

При ошибке на шаге 5: `room.Leave(peer)`, `peer.Stop()`. При **Replace** ошибке старый peer остаётся в комнате — `Revoke` не вызывается; `Room.Replace` откатывает `peers` и policy (`OnJoin(old)`). При любой ошибке join delivery шлёт **один binary-кадр** с OpCode ошибки (`deliveryerrors.SendJoinReject`), затем закрывает transport (`defer Close` на WS, `sess.close` на ENet). До `peer.Start()` use case **не** вызывает `peer.Stop()` — только откат membership в `Room`.

Ошибки join оборачивают sentinel из `domain/join_errors.go` (`%w`); маппинг в OpCode — **только** в `internal/delivery/errors` (чеклист — [.cursor/rules/join-error-opcodes.mdc](.cursor/rules/join-error-opcodes.mdc)).

Возвращает `(RoomSummary, PeerID, error)` — `PeerID` нужен для `LeaveRoom` при disconnect.

| Слой | Роль |
|------|------|
| `delivery/ws`, `delivery/enet` | Транспорт, извлечение token, `JoinRoom`, при отключении — `LeaveRoom` |
| `JoinRoomUseCase` | validate → комната → peer → join/replace → start |
| `LeaveRoomUseCase` | get peer → leave → stop → revoke (идемпотентно) |
| `Admission` | `Issue` / `Validate` (криптография ticket) |
| `Reservation` | слоты и capacity; отдельно от Admission |
| `adapter/realtime` | `Room`, `Peer`, `RoomPolicy` |

### Поток выхода (data plane)

`internal/usecase/room/leave_room.go` — `LeaveRoom(ctx, roomID, peerID)`.

- **WebSocket:** после `connection.Wait()` (клиент отключился) → `LeaveRoom`.
- **ENet:** `EventDisconnect`, если сессия была admitted → `LeaveRoom`, затем `sess.close()`.

### Бронирование слотов (Reservation)

Порт: `internal/port/reservation/reservation.go`.  
Реализация: `internal/adapter/reservation` (in-memory).

| Метод | Кто вызывает (цель) | Смысл |
|-------|---------------------|--------|
| `RegisterRoom` / `UnregisterRoom` | `Create` / `Delete` | capacity, lifecycle комнаты в учёте |
| `Reserve` | `IssueTicket` | бронь слота до join, `expiresAt` = TTL ticket |
| `Admit` | `JoinRoom` после `Validate` | `reserved` → `admitted` (слот остаётся в лимите) |
| `Revoke` | `LeaveRoom`, откат Issue/Join, orphan cleanup в Issue | освободить слот |
| `State` | `IssueTicket` (orphan cleanup) | `ReservationSlot`: none / reserved / admitted |

Поведение `Revoke`:

- **peer нет в map** → `nil` (идемпотентный disconnect / повторный Leave);
- **комната не зарегистрирована** → `ErrReservationNotFound` (строго: control plane должен был вызвать `RegisterRoom`).

Просроченные `reserved` снимаются при следующем `Reserve` (lazy sweep) по `expiresAt`. **`admitted` без TTL** — сессия живёт до `Leave` / eviction / orphan cleanup в Issue (слот admitted, peer не в room).

**Lifecycle:** Create — `RegisterRoom` → `CreateRoom` (откат `UnregisterRoom` + `errors.Join`); Delete — `DeleteRoom` → `UnregisterRoom`; shutdown — `GetList` → `Delete` по каждой комнате → `registry.Shutdown`.

Ошибки: `internal/domain/reservation_error.go`, `join_errors.go` (`ErrTicketSlotHeld`, `ErrPeerAlreadyInRoom`). Wire: `delivery/errors/reservation_reject.go` (`0x70–0x7F`); join-маппер делегирует reservation-ошибки в тот же диапазон.

### Допуск (Admission)

Порт: `internal/port/admission/admission.go`.  
Реализация: `internal/adapter/admission` (HMAC-SHA256, **token v2**, переменная длина).

**Формат ticket v2:** `version(1)` + `roomID(8)` + `peerID(8)` + `issuedAt(8)` + `expiresAt(8)` + `nickLen(1)` + `nick(nickLen)` + `HMAC-SHA256(32)`.  
Размер: `67 + len(nick) … 130` байт при `nick` 1…64 байта.

- **`Issue`** — `roomID`, `peerID`, `nickName`, `password` → `[]byte` ticket (`domain.ValidateNickName`).
- **`Validate`** — `token` → `domain.Claims` или ошибка (`domain.ErrInvalidToken`, `domain.ErrExpiredToken`, …).

`domain.Claims`: `RoomID`, `PeerID`, `NickName`, `IssuedAt`, `ExpiresAt`.

В `main` секрет зашит как `dev-secret-change-me` (только для разработки).

### Realtime

- **`Room`** — один event loop на комнату: `incoming` (кадры), `lifecycle` (Join/Leave/Replace), full/patch ticks из `RoomPolicy.TickIntervals()`.
- **`Peer`** — чтение/запись кадров через `transport.Connection`; `NickName` из ticket → `PeerState` на join.
- **`RoomPolicy`** (`port/realtime/policy`) — `OnStart`/`OnStop`, `OnJoin`/`OnLeave`, `OnMessage`, `OnTickFullState`/`OnTickPatchState`.
- **`StateRoomPolicy`** (`adapter/realtime/policy/state`) — authoritative `RoomState`: master election (min ping ≥ 0), full/patch broadcast, master-only entities/input/RPC, in-room errors отправителю.
- `StateRoomPolicyFactory` — дефолтные интервалы тиков (4s full, 50ms patch); `0` в конструкторе → подстановка дефолтов.
- Patch apply — атомарно на уровне entities map, entity components и component values (`ApplyMapStatePatchCopy`).

### Реестр комнат

- Порт: `internal/port/registry/room_registry.go`
- Реализация: `internal/adapter/registry/room_registry.go` (in-memory `map[RoomID]Room`, lifecycle-`ctx` для комнат)
- `Shutdown` — остановка всех комнат при завершении процесса

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

**Reservation (`0x70–0x7F`)** — control / admit по слоту; маппер `ReservationRejectOpCode` / `SendReservationReject` (также через `JoinRejectOpCode` для reservation-ошибок на join):

| OpCode | Hex | Смысл |
|--------|-----|--------|
| `OpReservationNotFound` | `0x70` | Комната/слот не в store |
| `OpReservationFull` | `0x71` | Нет свободных слотов |
| `OpReservationSlotHeld` | `0x72` | Слот занят (reserved/admitted) |
| `OpReservationNotReserved` | `0x73` | Admit без брони |
| `OpReservationExpired` | `0x74` | Бронь истекла |
| `OpReservationAlreadyAdmitted` | `0x75` | Уже admitted |
| `OpPeerAlreadyInRoom` | `0x76` | Peer в room (Issue, политика A) |
| `OpReservationInternal` | `0x7F` | Прочая ошибка reservation |

**In-room (`0x60–0x6F`)** — после admit; ошибка **отправителю**, соединение остаётся открытым; маппер `InRoomErrorOpCode` / `SendInRoomError` (`delivery/errors/join_reject.go`):

| OpCode | Hex | Sentinel (`domain`) |
|--------|-----|---------------------|
| `OpInRoomInternal` | `0x60` | `ErrInRoomInternal` (default) |
| `OpInRoomNotMaster` | `0x61` | `ErrNotMaster` |
| `OpInRoomInvalidPayload` | `0x62` | `ErrInRoomInvalidPayload` |
| `OpInRoomInvalidRpc` | `0x63` | `ErrInvalidRpcTarget` |
| `OpInRoomRpcPeerNotFound` | `0x64` | `ErrRpcTargetPeerNotFound` |
| `OpInRoomNoMaster` | `0x65` | `ErrNoMaster` |
| `OpInRoomUnknownOpcode` | `0x66` | `ErrInRoomUnknownOpcode` |

**State sync (игровой трафик):**

| OpCode | Hex | Смысл |
|--------|-----|--------|
| `OpCodeFullState` | `0x01` | Room snapshot |
| `OpCodePatchState` | `0x02` | Room patch |
| `OpCodeFullEntities` | `0x03` | Entities map (master → server) |
| `OpCodePatchEntities` | `0x04` | Entities patch |
| `OpCodeFullInput` | `0x05` | Input map |
| `OpCodePatchInput` | `0x06` | Input patch |
| `OpCodeRpc` | `0x07` | RPC (master → target peer) |

Layout payload — [docs/wire-state-codec.md](docs/wire-state-codec.md).

**State wire (payload layout):** [docs/wire-state-codec.md](docs/wire-state-codec.md) — BE, map, flags, лимиты; реализация `internal/adapter/realtime/codec/`.

Источник OpCode: state — `internal/domain/state/opcodes.go`; join/reservation/in-room — `internal/domain/opcodes.go`, `join_errors.go`, `room_errors.go`.  
Мапперы: `internal/delivery/errors` — `join_reject.go` (join + in-room), `reservation_reject.go` (импорт: `deliveryerrors ".../delivery/errors"`).

## API и endpoints

### HTTP (Fiber), порт `:3000`

| Метод | Путь | Use case | Успех | Тело запроса | Тело ответа |
|-------|------|----------|-------|--------------|-------------|
| `POST` | `/rooms` | `Create` | `201` | `{"id":1,"capacity":8,"password":"optional"}` | `{"id":1,"peers":[]}` |
| `GET` | `/rooms` | `GetList` | `200` | — | `{"rooms":[…]}` |
| `DELETE` | `/rooms/:id` | `Delete` | `204` | `{"password":"…"}` (тело опционально; нужно, если при create задан пароль) | — |
| `POST` | `/rooms/:id/tickets` | `IssueTicket` | `201` | `{"nick_name":"player","password":"…"}` | `{"token":"<base64url>"}` |

`nick_name` — **обязателен**, 1…64 байта UTF-8, без control-символов (`domain.ValidateNickName`). При ошибке — `400` / `invalid_nick_name`.

Если при создании комнаты `password` не пустой — тот же пароль обязателен для `IssueTicket` и `Delete`. Пустой/отсутствующий пароль при create — комната без защиты.

Ошибки: JSON `{"code":"…","message":"…"}`; коды — `room_not_found`, `room_already_exists`, `ticket_slot_held`, `invalid_nick_name`, `invalid_request`, … (маппинг в `delivery/http/errors.go`).

Ticket в ответе — **base64url без padding** (сырой HMAC-ticket v2, 67…130 байт в зависимости от длины nick). Для WS query декодируйте base64url в сырые байты.

### WebSocket

| Метод | Путь | Параметры | Поведение |
|-------|------|-----------|-----------|
| `GET` | `/ws` | `token` (query) | Upgrade → `JoinRoom` → ожидание disconnect → `LeaveRoom` |

При ошибке `JoinRoom`: один кадр с OpCode из таблицы выше → закрытие WebSocket.

**Ограничение:** ticket бинарный (v2, переменная длина); в HTTP — base64url. Для WS query — декодируйте в сырые байты. Отсутствие `token` в query — закрытие **без** кадра (`0x40` не отправляется).

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
  port/                         — admission, registry, reservation, realtime, transport, logging
  usecase/room/
    create.go, delete.go, get_list.go
    issue_ticket.go             — IssueTicketUseCase
    join_room.go                — JoinRoomUseCase
    leave_room.go               — LeaveRoomUseCase
    room_summary.go, peer_summary.go
  adapter/
    admission/                  — HMAC ticket
    reservation/                — in-memory слоты
    realtime/                   — Room, Peer; policy/state (StateRoomPolicy)
    registry/
    transport/ws|enet/
    logging/stdlib|zap/
  delivery/
    http/                        — dto, errors, rooms handlers
    ws/rooms.go
    enet/                       — handler, config, run_cgo / run_stub
    errors/                     — wire OpCode: join (0x40+), in-room (0x60+), reservation (0x70+)
    ws/rooms.go, enet/          — транспорт → JoinRoom use case
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

REST control plane — JSON (см. таблицу endpoints).

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

- Конфигурация admission: секрет и пароль комнаты из env (сейчас dev secret в `main`)
- Конфигурация: порт HTTP/ENet, секрет admission из env
- Health-check
- Идемпотентный `LeaveRoom` в realtime, если peer уже снят с `Room`
- Ping/idle eviction для зомби `admitted` (сейчас — события + orphan cleanup в Issue)
- `delivery/ws` e2e: issue ticket → join по WS
- Буферизация `Room.incoming` под нагрузку (сейчас unbuffered)
