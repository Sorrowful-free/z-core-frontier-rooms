# delivery/enet

UDP host (отдельный порт от Fiber). Build tag `enet` + CGO.

**Клиентская спека (Godot, основной транспорт):** [docs/client-protocol/enet.md](../../../docs/client-protocol/enet.md).

## По умолчанию (`DefaultConfig`)

| Параметр | Значение |
|----------|----------|
| Порт | `7777` |
| Peer limit | `64` |
| Channel limit | `2` |
| Первый пакет | сырой ticket (`admit`), max `TRANSPORT_MAX_INCOMING_FRAME_BYTES` |
| После admit | channel `0`, `[OpCode u8][payload]`; server send reliable |
| Oversized packet/frame | disconnect (+ `LeaveRoom` если уже admitted) |
| Ошибка admit | OpCode `0x40`–`0x7F` (1 байт) → disconnect |

Без `enet`/cgo: `Listen()` — warn, host не поднимается.

```bash
CGO_ENABLED=1 go build -tags enet -o bin/rooms-enet ./cmd/rooms
CGO_ENABLED=1 go test -tags enet ./tests/delivery/enet/...
```

Поток join/leave — тот же `JoinRoomUseCase` / `LeaveRoomUseCase`, что WS.
