# z-core-frontier-rooms

Сервис игровых комнат: **control plane** (HTTP, [Fiber](https://gofiber.io/) v3) и **data plane** (WebSocket, ENet).

- Хранение комнат и peers, ticket с `nick_name`, authoritative `RoomState` (`StateRoomPolicy`).
- Подробности по слоям — в **пакетных README** (см. ниже), не дублируются здесь.

## Быстрый старт

```bash
# См. .env.example — ADMISSION_SECRET обязателен (≥ 32 байт)
export ADMISSION_SECRET=replace-with-random-secret-at-least-32-bytes-long!!
export HTTP_API_KEY=replace-with-random-api-key-16-chars-min

go build -o bin/rooms ./cmd/rooms
go run ./cmd/rooms
```

- HTTP + WS: `http://localhost:3000`
- ENet (опционально): UDP `7777` — `CGO_ENABLED=1 go build -tags enet ./cmd/rooms`

```bash
go test ./tests/...
# ENet cgo (gcc + CGO_ENABLED=1):
CGO_ENABLED=1 go test -tags enet ./tests/delivery/enet/...
```

Требования: Go 1.25+; ENet — CGO, `github.com/codecat/go-enet`, tag `enet`.

## Архитектура

[docs/architecture.md](docs/architecture.md) — слои, зависимости, типичный поток ticket → join → leave.

## Документация по пакетам

При изменении пакета обновляйте его `README.md` — [.cursor/rules/package-docs.mdc](.cursor/rules/package-docs.mdc).

| Область | Документ |
|---------|----------|
| **Client protocol (Godot)** | [docs/client-protocol/README.md](docs/client-protocol/README.md) → [enet.md](docs/client-protocol/enet.md) (основной), [llms.txt](docs/llms.txt) |
| Composition root | [cmd/rooms/README.md](cmd/rooms/README.md) |
| Домен | [internal/domain/README.md](internal/domain/README.md) |
| State model | [internal/domain/state/README.md](internal/domain/state/README.md) |
| Порты | [internal/port/README.md](internal/port/README.md) |
| Use cases | [internal/usecase/room/README.md](internal/usecase/room/README.md) |
| Config (env) | [internal/adapter/config/README.md](internal/adapter/config/README.md) |
| HTTP auth | [internal/adapter/httpauth/README.md](internal/adapter/httpauth/README.md) |
| HTTP limits | [internal/adapter/httplimits/README.md](internal/adapter/httplimits/README.md) |
| Admission (ticket v2) | [internal/adapter/admission/README.md](internal/adapter/admission/README.md) |
| Reservation | [internal/adapter/reservation/README.md](internal/adapter/reservation/README.md) |
| Room registry | [internal/adapter/registry/README.md](internal/adapter/registry/README.md) |
| Realtime (Room, Peer) | [internal/adapter/realtime/README.md](internal/adapter/realtime/README.md) |
| StateRoomPolicy | [internal/adapter/realtime/policy/state/README.md](internal/adapter/realtime/policy/state/README.md) |
| Wire codec | [internal/adapter/realtime/codec/README.md](internal/adapter/realtime/codec/README.md) → [docs/wire-state-codec.md](docs/wire-state-codec.md) |
| Delivery (индекс) | [internal/delivery/README.md](internal/delivery/README.md) |
| HTTP API | [internal/delivery/http/README.md](internal/delivery/http/README.md) |
| WebSocket | [internal/delivery/ws/README.md](internal/delivery/ws/README.md) |
| ENet | [internal/delivery/enet/README.md](internal/delivery/enet/README.md) |
| Wire OpCodes | [internal/delivery/errors/README.md](internal/delivery/errors/README.md) |
| Тесты | [tests/README.md](tests/README.md) |

## Правила проекта

- Go, Fiber, тесты в `tests/`: [.cursor/rules/go-standards.mdc](.cursor/rules/go-standards.mdc)
- Конфигурация: [.cursor/rules/config.mdc](.cursor/rules/config.mdc)
- Пакетная документация: [.cursor/rules/package-docs.mdc](.cursor/rules/package-docs.mdc)
- Join / in-room OpCode чеклист: [.cursor/rules/join-error-opcodes.mdc](.cursor/rules/join-error-opcodes.mdc)

## TODO

- Health-check
- Ping/idle eviction для zombie peers (отдельный пункт после hardening)
- WS e2e: issue → join
