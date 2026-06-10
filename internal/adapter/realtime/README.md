# adapter/realtime

Runtime комнаты: `Room`, `Peer`, фабрики.

## Room

Один goroutine event loop (`processRoomEvents`):

- `incoming` — кадры от peers → `RoomPolicy.OnMessage`
- `lifecycle` — Join / Leave / Replace (сериализация с OnMessage)
- tickers — `OnTickFullState` / `OnTickPatchState` из `policy.TickIntervals()`

Публичные операции:

- `Join` — peers map + `OnJoin`
- `Leave` — `OnLeave` + удаление из map
- `Replace` — `OnLeave(old)` → swap peer → `OnJoin(new)`; при ошибке join — rollback map + `OnJoin(old)`
- `Send` / `Deliver` — исходящие `PeerEvent`

## Peer

- `NickName` задаётся в `NewPeer` из ticket (`JoinRoom` → `claims.NickName`)
- I/O через `transport.Connection`
- `Ping()` для refresh в policy patch tick

## Policy

Контракт: `port/realtime/policy.RoomPolicy`.  
Реализация: [policy/state/README.md](policy/state/README.md).

## Codec

Wire encode/decode: [codec/README.md](codec/README.md).

## In-room errors

`room_in_room_error.go` — при ошибке `OnMessage` отправителю кадр OpCode (`delivery/errors`).
