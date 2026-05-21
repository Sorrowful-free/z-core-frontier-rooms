# z-core-frontier-rooms

Сервис игровых комнат: control plane по HTTP ([Fiber](https://gofiber.io/)) и data plane по WebSocket и ENet. Проект в активной разработке — ниже зафиксировано **текущее** состояние кода и задуманные границы слоёв.

## Статус

| Область | Состояние |
|--------|-----------|
| Домен, порты, adapter realtime (Room, Peer, RelayHandler) | Черновик реализован |
| Use case `Connect` | Реализован (оркестрация входа в комнату) |
| Порт `Admission` | Только интерфейс, реализаций нет |
| `RoomRegistry` | Только интерфейс, реализаций нет |
| Delivery (HTTP / WS / ENet) | Заготовки пакетов, handlers не подключены |
| `cmd/rooms` | Заглушка: wiring слушателей — TODO |

Сборка и `go run` выполняются, но процесс пока не поднимает сеть и не обслуживает клиентов.

## Назначение

- Хранить и отдавать **комнаты** (`Room`) с набором подключённых **пиров** (`Peer`).
- **Допуск** в комнату: выдача билета (ticket) и проверка при подключении по realtime-транспорту.
- **Relay** кадров между пирами в одной комнате (через `RoomHandler` / `RelayHandler`).
- Единый контракт транспорта (`transport.Connection`) для WebSocket и ENet; различия протокола — только в `delivery`.

## Архитектура (слои)

```text
cmd/rooms          — composition root (DI, запуск слушателей) [TODO]

delivery/          — inbound: HTTP, WebSocket, ENet (парсинг протокола, lifecycle соединения)
usecase/           — сценарии (оркестрация портов)
port/              — интерфейсы (admission, registry, realtime, transport, logging)
domain/            — типы и события без зависимостей от инфраструктуры
adapter/           — реализации портов (realtime, transport, logging)
```

Зависимости направлены внутрь: `delivery` → `usecase` → `port` ← `adapter`, `domain` не импортирует остальные слои.

### Поток подключения к комнате (целевой)

Реализован в `internal/usecase/room/connect.go`. Delivery после handshake передаёт сюда уже извлечённый `token` и `transport.Connection`.

```text
1. admission.Validate(ctx, token) → domain.Claims
2. roomRegistry.GetRoom(claims.RoomID)
3. peerFactory.CreatePeer(claims.PeerID, connection, logger)
4. room.Join(peer)
5. peer.Start()
```

При ошибке на шагах 4–5 use case откатывает peer (`peer.Stop()`). Закрытие сокета на уровне delivery при `error` из `Connect` — ответственность inbound-адаптера.

**Разделение ролей:**

| Слой | Что делает |
|------|------------|
| `delivery` (ws / enet) | Понимает протокол транспорта, извлекает ticket, вызывает use case, при ошибке закрывает соединение |
| `Admission` | Проверяет ticket (подпись, срок, содержимое claims), без знания WS/ENet |
| `ConnectUseCase` | Прослойка между транспортом и realtime: validate → комната → peer → join → start |
| `adapter/realtime` | Исполнение Room/Peer, relay через `RelayHandler` |

### Допуск (Admission)

Порт `internal/port/admission/admission.go`:

- **`Issue`** — control plane (планируется HTTP): пароль комнаты, идентификаторы → выдача credentials (сейчас в контракте возвращаются `domain.Claims`; сериализованный ticket для клиента — при появлении реализации).
- **`Validate`** — data plane: `token []byte` после парсинга handshake → `Claims` или ошибка.

`domain.Claims` содержит `RoomID`, `PeerID`, `IssuedAt`, `ExpiresAt`.

### Realtime

- **`Room`** — жизненный цикл комнаты, `Join` / `Leave`, рассылка `PeerEvent`, приём `RoomEvent`.
- **`Peer`** — привязка к `transport.Connection`, goroutines приёма/отправки кадров.
- **`RoomHandler`** — хуки (`OnStart`, `OnJoin`, `OnMessage`, …). Реализация **`RelayHandler`** ретранслирует кадры между пирами.
- **`PeerFactory`** / **`RoomFactory`** — порты; реализации в `internal/adapter/realtime/`.

### Реестр комнат

`internal/port/registry/room_registry.go`: `GetRoom`, `CreateRoom`, `DeleteRoom`. Реализация и политика «комната должна существовать до connect» — TODO.

### Транспорт

`internal/port/transport/connection.go` — единый интерфейс: `Send` / `GetIncoming` / `Close` над `domain.Frame`.

Адаптеры:

- `internal/adapter/transport/ws/` — WebSocket
- `internal/adapter/transport/enet/` — ENet (`connection_cgo.go` / `connection_stub.go` для сборки без CGO)

### Delivery (заготовки)

| Пакет | Назначение |
|-------|------------|
| `internal/delivery/http` | Fiber API: создание комнат, `Admission.Issue` |
| `internal/delivery/ws` | WebSocket handshake → `ConnectUseCase` |
| `internal/delivery/enet` | ENet handshake → `ConnectUseCase` |

Пакеты объявлены; маршруты и обработчики — впереди.

## Структура репозитория

```text
cmd/rooms/                 — точка входа
internal/
  domain/                  — RoomID, PeerID, Frame, Claims, events
  port/                    — admission, registry, realtime, transport, logging
  usecase/room/            — ConnectUseCase
  adapter/
    realtime/              — Room, Peer, factories, RelayHandler
    transport/ws|enet/     — обёртки соединений
    logging/stdlib/        — slog-адаптер Logger
  delivery/http|ws|enet/   — inbound HTTP / WS / ENet [TODO]
tests/                     — тесты (зеркало слоёв), см. tests/README.md
```

## Требования

- Go 1.25+

## Сборка

```bash
go build -o bin/rooms ./cmd/rooms
```

## Запуск и эксплуатация

<!-- TODO: дополнить, когда появится wiring в cmd/rooms -->

_Раздел в работе. Планируется описать:_

- переменные окружения и конфигурация;
- порты HTTP / WebSocket / ENet;
- зависимости (CGO для ENet, если требуется);
- пример локального запуска и health-check;
- graceful shutdown.

Пока точка входа только логирует старт и завершается без поднятия слушателей:

```bash
go run ./cmd/rooms
```

## Тесты

Тесты лежат в [`tests/`](tests/), не рядом с production-кодом в `internal/` и `cmd/`.

```bash
go test ./tests/...
```

Подробнее — [tests/README.md](tests/README.md).

## Соглашения проекта

- HTTP — только [Fiber](https://gofiber.io/) v3.
- Тесты — только под `tests/`, внешние пакеты `*_test`.
- Правила для агентов и линтеров — [.cursor/rules/go-standards.mdc](.cursor/rules/go-standards.mdc).
