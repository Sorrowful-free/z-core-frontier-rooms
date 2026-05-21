# z-core-frontier-rooms

Сервис игровых комнат: **control plane** по HTTP ([Fiber](https://gofiber.io/) v3) и **data plane** по WebSocket и ENet. Ниже — **текущее** состояние кода и границы слоёв.

## Статус

| Область | Состояние |
|--------|-----------|
| Домен, порты, `adapter/realtime` (Room, Peer, RelayHandler) | Черновик реализован |
| `adapter/registry` (`RoomRegistry`) | In-memory реализация (`CreateRoom`, `GetRoom`, `DeleteRoom`, `GetList`) |
| Use case `Connect` | Реализован, **не** подключён в `cmd` / `delivery/ws` |
| Use case `Create` / `Delete` / `GetList` | Реализованы, подключены к HTTP (handlers — заглушки) |
| Use case `Join` | Реализован (`Admission.Issue` → token), в `cmd` передан `nil` admission; handler `JoinRoom` есть, маршрут **не** зарегистрирован |
| Порт `Admission` | Интерфейс; реализаций нет |
| `delivery/http` | `RoomsHandler`, маршруты REST (см. ниже) |
| `delivery/ws` | `RoomsHandler` + DI `ConnectUseCase`; `RegisterRoutes` и wiring в `cmd` — нет |
| `delivery/enet` | Заготовка пакета |
| `cmd/rooms` | Fiber + registry + HTTP routes → `Listen(":3000")` |
| Graceful shutdown, конфиг порта, ENet-слушатель | TODO |

Сборка и `go run` поднимают HTTP на порту **3000**; обработчики пока не вызывают use case и не отдают JSON.

## Назначение

- Хранить и отдавать **комнаты** (`Room`) с набором подключённых **пиров** (`Peer`).
- **Допуск** в комнату: выдача ticket (HTTP / `Join`) и проверка при подключении по realtime-транспорту (`Connect`).
- **Relay** кадров между пирами в одной комнате (через `RoomHandler` / `RelayHandler`).
- Единый контракт транспорта (`transport.Connection`) для WebSocket и ENet; различия протокола — только в `delivery`.

## Архитектура (слои)

```text
cmd/rooms              — composition root: fiber.New(), DI, RegisterRoutes, Listen

delivery/http          — REST control plane (RoomsHandler)
delivery/ws            — WebSocket upgrade → ConnectUseCase [в разработке]
delivery/enet          — ENet inbound → ConnectUseCase [заготовка]

usecase/room/          — Create, Delete, GetList, Join, Connect

port/                  — admission, registry, realtime, transport, logging
domain/                — типы и события без зависимостей от инфраструктуры
adapter/               — realtime, registry, transport (ws/enet), logging
```

Зависимости направлены внутрь: `delivery` → `usecase` → `port` ← `adapter`; `domain` не импортирует остальные слои.

### Composition root (`cmd/rooms`)

Один экземпляр `*fiber.App` создаётся в `main` (отдельного `Server` в `delivery/http` нет):

```text
fiber.New()
  → logger, roomFactory, relayRoomHandlerFactory, roomRegistry
  → CreateUseCase, JoinUseCase (admission=nil), DeleteUseCase, GetListUseCase
  → http.RoomsHandler.RegisterRoutes(app)
  → (план) ws.RoomsHandler.RegisterRoutes(app) — тот же app
  → app.Listen(":3000")
```

WebSocket планируется на **том же** Fiber-приложении (HTTP upgrade). ENet — отдельный слушатель, не Fiber.

### Поток входа в комнату (data plane)

Реализован в `internal/usecase/room/connect.go`. `delivery/ws` или `delivery/enet` после handshake передаёт `token` и `transport.Connection`.

```text
1. admission.Validate(ctx, token) → domain.Claims
2. roomRegistry.GetRoom(claims.RoomID)
3. peerFactory.CreatePeer(claims.PeerID, connection, logger)
4. room.Join(peer)
5. peer.Start()
```

При ошибке на шагах 4–5 use case откатывает peer (`peer.Stop()`, `room.Leave`). Закрытие сокета при ошибке из `Connect` — ответственность inbound-адаптера.

| Слой | Роль |
|------|------|
| `delivery/ws`, `delivery/enet` | Протокол транспорта, извлечение ticket, вызов `Connect`, закрытие соединения при ошибке |
| `Admission` | `Issue` / `Validate` ticket, без знания WS/ENet |
| `ConnectUseCase` | validate → комната → peer → join → start |
| `adapter/realtime` | Room, Peer, relay через `RelayHandler` |
| `adapter/transport/ws` | `WsConnection` над `gofiber/contrib/v3/websocket` |

### Поток выдачи ticket (control plane)

`internal/usecase/room/join.go` — `Join` → `admission.Issue` → `[]byte` (ticket для клиента).

Планируется HTTP-handler `JoinRoom` (объявлен в `delivery/http`, маршрут в `RegisterRoutes` пока не добавлен). Клиент затем подключается по WS, например `GET /ws?token=...` → `ConnectUseCase`.

### Допуск (Admission)

`internal/port/admission/admission.go`:

- **`Issue`** — control plane: `roomID`, `peerID`, `password` → ticket (`[]byte`).
- **`Validate`** — data plane: `token []byte` → `domain.Claims` или ошибка.

`domain.Claims`: `RoomID`, `PeerID`, `IssuedAt`, `ExpiresAt`.

### Realtime

- **`Room`** — жизненный цикл, `Join` / `Leave`, события пиров.
- **`Peer`** — `transport.Connection`, приём/отправка кадров.
- **`RoomHandler`** — хуки; **`RelayHandler`** ретранслирует кадры между пирами.
- Фабрики — `internal/adapter/realtime/`.

### Реестр комнат

- Порт: `internal/port/registry/room_registry.go`
- Реализация: `internal/adapter/registry/room_registry.go` (in-memory `map[RoomID]Room`)

### Транспорт

`internal/port/transport/connection.go` — `Send` / `GetIncoming` / `Close` над `domain.Frame`.

| Адаптер | Пакет |
|---------|--------|
| WebSocket | `internal/adapter/transport/ws/` (`NewWsConnection`) |
| ENet | `internal/adapter/transport/enet/` (CGO / stub без CGO) |

### Delivery

| Пакет | Состояние |
|-------|-----------|
| `internal/delivery/http` | `RoomsHandler`: `RegisterRoutes` на `*fiber.App`; методы — заглушки |
| `internal/delivery/ws` | Структура handler + `ConnectUseCase`; регистрация маршрутов и `websocket.New` — TODO |
| `internal/delivery/enet` | Заготовка пакета |

## HTTP API (текущие маршруты)

Базовый URL: `http://localhost:3000` (порт зашит в `cmd/rooms/main.go`).

| Метод | Путь | Handler | Use case |
|-------|------|---------|----------|
| `POST` | `/rooms` | `CreateRoom` | `Create` |
| `GET` | `/rooms` | `GetListRooms` | `GetList` |
| `DELETE` | `/rooms/:id` | `DeleteRoom` | `Delete` |

`JoinRoom` (use case `Join`) в коде есть, в `RegisterRoutes` **не** объявлен.

## Структура репозитория

```text
cmd/rooms/                    — точка входа, Fiber, DI, Listen
internal/
  domain/                     — RoomID, PeerID, Frame, Claims, events
  port/                       — admission, registry, realtime, transport, logging
  usecase/room/               — Create, Delete, GetList, Join, Connect; RoomSummary
  adapter/
    realtime/                 — Room, Peer, factories, RelayHandler
    registry/                 — RoomRegistry (in-memory)
    transport/ws|enet/
    logging/stdlib/
  delivery/
    http/rooms.go             — RoomsHandler, RegisterRoutes
    ws/rooms.go               — RoomsHandler (Connect), wiring TODO
    enet/                     — заготовка
tests/                        — тесты (зеркало слоёв), см. tests/README.md
```

## Требования

- Go 1.25+

## Сборка

```bash
go build -o bin/rooms ./cmd/rooms
```

## Запуск

```bash
go run ./cmd/rooms
```

Сервис слушает **`:3000`**. Ответы REST пока не реализованы (handlers возвращают пустой успех).

Планируется дополнить:

- переменные окружения и конфигурация порта;
- регистрация WebSocket на том же Fiber `app`;
- отдельный слушатель ENet;
- graceful shutdown;
- health-check.

## Тесты

Тесты — в [`tests/`](tests/), не рядом с `internal/` и `cmd/`.

```bash
go test ./tests/...
```

Подробнее — [tests/README.md](tests/README.md). Пока тестовых пакетов с кодом нет, только описание структуры.

## Соглашения проекта

- HTTP — только [Fiber](https://gofiber.io/) v3; один `*fiber.App` в `cmd` для REST и WS upgrade.
- Handlers в `delivery` тонкие: парсинг → use case → JSON / маппинг ошибок.
- Тесты — только под `tests/`, внешние пакеты `*_test`.
- Правила для агентов — [.cursor/rules/go-standards.mdc](.cursor/rules/go-standards.mdc).

## TODO (ближайшее)

- Реализация `Admission`
- HTTP: тела запросов/ответов, вызов use case, маршрут Join
- WS: `RegisterRoutes`, `websocket.New`, wiring `ConnectUseCase` + `peerFactory` в `cmd`
- ENet delivery и слушатель
- Конфиг, graceful shutdown
