# adapter/realtime

Runtime комнаты: `Room`, `Peer`, фабрики.

## Room

Один goroutine event loop (`processRoomEvents`):

- `incoming` — буфер `RoomIncomingQueueSize` (default 128); полный → drop (`domain.ErrQueueFull`); кадры → `OnMessage`
- `lifecycle` — Join / Leave / Replace (сериализация с OnMessage)
- tickers — `OnTickFullState` / `OnTickPatchState` из `policy.TickIntervals()`

Публичные операции:

- `Join` — peers map + `OnJoin`
- `Leave` — `OnLeave` + удаление из map
- `Replace` — `OnLeave(old)` → swap peer → `OnJoin(new)`; при ошибке join — rollback map + `OnJoin(old)`
- `Send` / `Deliver` — исходящие `PeerEvent` / входящие `RoomEvent`

### Контракт `Deliver`

| Исход | `Room.Deliver` / `Peer.Deliver` | Поведение вызывающего |
|-------|----------------------------------|------------------------|
| Успех | `nil` | — |
| Очередь полна | `domain.ErrQueueFull` | логировать; **не** рвать соединение (`processIncomingEvents` продолжает чтение) |
| Остановлен | ошибка `room stopped` / `peer stopped` | фатально для peer loop (кроме уже остановленного) |

`Room.Send` при `ErrQueueFull` у отдельного peer — `Warn` и продолжает broadcast остальным (full-state/patch не падают целиком из-за одного переполненного outbound).

## Shutdown

`Room.Stop()` завершает event loop через отмену контекста комнаты (`cancel`); `processRoomEvents` выходит по `<-ctx.Done()`.

Каналы `incoming` и `lifecycle` **намеренно не закрываются**: в них пишут внешние горутины (peers, use cases), не входящие в `Room.wg`. Закрытие после `wg.Wait()` приводило к панике `send on closed channel`.

После cancel продюсеры (`Deliver`, `dispatchLifecycle`) возвращают ошибку «room stopped» через `ctx.Err()`, не блокируясь и не паникуя.

Тесты: `tests/adapter/realtime/room_stop_test.go`, `tests/adapter/registry/room_registry_stop_test.go` (concurrent `Deliver`/`Join`/`Leave` + `Stop`).

## Peer

- `NickName` задаётся в `NewPeer` из ticket (`JoinRoom` → `claims.NickName`)
- I/O через `transport.Connection`
- `outbound` — буфер `PeerOutboundQueueSize` (default 128); полный → `domain.ErrQueueFull` (room loop не стопорится)
- `Ping()` для refresh в policy patch tick

## Policy

Контракт: `port/realtime/policy.RoomPolicy`.  
Реализация: [policy/state/README.md](policy/state/README.md).

## Codec

Wire encode/decode: [codec/README.md](codec/README.md).

## In-room errors

`room_in_room_error.go` — при ошибке `OnMessage` отправителю кадр OpCode (`delivery/errors`).
