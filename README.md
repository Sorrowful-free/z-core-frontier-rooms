# z-core-frontier-rooms

Сервис игровых комнат: **control plane** (HTTP, [Fiber](https://gofiber.io/) v3) и **data plane** (WebSocket, ENet).

- Хранение комнат и peers, ticket с `nick_name`, authoritative `RoomState` (`StateRoomPolicy`).
- Подробности по слоям — в **пакетных README** (см. ниже), не дублируются здесь.

## Быстрый старт

```bash
go build -o bin/rooms ./cmd/rooms
go run ./cmd/rooms
```

- HTTP + WS: `http://localhost:3000`
- ENet (опционально): UDP `7777` — `CGO_ENABLED=1 go build -tags enet ./cmd/rooms`

```bash
go test ./tests/...
```

Требования: Go 1.25+; ENet — CGO, `github.com/codecat/go-enet`, tag `enet`.

## Архитектура

[docs/architecture.md](docs/architecture.md) — слои, зависимости, типичный поток ticket → join → leave.

## Документация по пакетам

При изменении пакета обновляйте его `README.md` — [.cursor/rules/package-docs.mdc](.cursor/rules/package-docs.mdc).

| Область | Документ |
|---------|----------|
| Composition root | [cmd/rooms/README.md](cmd/rooms/README.md) |
| Домен | [internal/domain/README.md](internal/domain/README.md) |
| State model | [internal/domain/state/README.md](internal/domain/state/README.md) |
| Порты | [internal/port/README.md](internal/port/README.md) |
| Use cases | [internal/usecase/room/README.md](internal/usecase/room/README.md) |
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
- Пакетная документация: [.cursor/rules/package-docs.mdc](.cursor/rules/package-docs.mdc)
- Join / in-room OpCode чеклист: [.cursor/rules/join-error-opcodes.mdc](.cursor/rules/join-error-opcodes.mdc)

## TODO

- Конфиг из env (секрет admission, порты)
- Health-check
- Идемпотентный `LeaveRoom` если peer уже снят
- Ping/idle eviction для zombie `admitted`
- WS e2e: issue → join
- Буфер `Room.incoming` под нагрузку
