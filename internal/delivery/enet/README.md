# delivery/enet

UDP host (отдельный порт от Fiber). Build tag `enet` + CGO.

## По умолчанию (`DefaultConfig`)

| Параметр | Значение |
|----------|----------|
| Порт | `7777` |
| Первый пакет | сырой ticket (`admit`) |
| Ошибка admit | OpCode `0x40`–`0x51` → disconnect |
| После admit | binary `OpCode` + payload |

Без `enet`/cgo: `Listen()` — warn, host не поднимается.

```bash
CGO_ENABLED=1 go build -tags enet -o bin/rooms-enet ./cmd/rooms
```

Поток join/leave — тот же `JoinRoomUseCase` / `LeaveRoomUseCase`, что WS.
