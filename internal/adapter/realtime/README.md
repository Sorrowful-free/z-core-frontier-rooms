# adapter/realtime

Runtime комнаты: `Room`, `Peer`, фабрики.

Фабрики (`NewRoomFactory`, `NewPeerFactory`) принимают `port/metrics.Metrics` (nil-safe) и вливают его полем в создаваемые `Room`/`Peer` — сигнатуры `NewRoom`/`NewPeer` остаются стабильными для white-box тестов (умолчание `Noop`). Эмит: joins/leaves + gauge `active_peers` на lifecycle-переходах (**по факту мутации `peers` map** - повторный join/leave того же peer'а не даёт дрейф, P19b), drops переполненных `incoming`/`outbound`, gauge-сэмплы глубины обеих очередей (`len(chan)` на enqueue/dequeue, приближение), duration тиков `rooms_room_tick_seconds{kind}`, ошибки send на живом соединении `rooms_peer_send_errors_total` (ожидаемый teardown `!IsAlive` не считается).

## Room

Один goroutine event loop (`processRoomEvents`):

- `incoming` — буфер `RoomIncomingQueueSize` (default 128); полный → drop (`domain.ErrQueueFull`); кадры → `OnMessage`
- `lifecycle` — Join / Leave / Replace (сериализация с OnMessage)
- tickers — `OnTickFullState` / `OnTickPatchState` из `policy.TickIntervals()`

Публичные операции:

- `Join` — capacity-check + вставка в peers map + `OnJoin` выполняются **атомарно на room-loop'е** (P13): peer виден в `r.peers` до того, как policy вставляет его в state — tick'и (`refreshPeerPings`) не могут принять новоприсоединившегося за stale. При ошибке `OnJoin` peers-вставка откатывается. Pre-check capacity на caller'е — ранний ответ, авторитетная проверка в loop'е.
- `Leave` — удаление из peers map + `OnLeave` атомарно на room-loop'е: peer вне реестра **до** отработки политики; ошибка `OnLeave` возвращается, state-запись при откате вычищается следующим prune-тиком (`GetPeer` → `ErrPeerNotFound` → stale).
- `Replace` — `lifecycleLeave(old)` → `lifecycleJoin(new)`; при ошибке join — rollback `lifecycleJoin(old)`
- `Start`/`Stop` — `sync.Once`-guards; повторные вызовы идемпотентны
- `Send` / `Deliver` — исходящие `PeerEvent` / входящие `RoomEvent`

### Контракт `Deliver`

| Исход | `Room.Deliver` / `Peer.Deliver` | Поведение вызывающего |
|-------|----------------------------------|------------------------|
| Успех | `nil` | — |
| Очередь полна | `domain.ErrQueueFull` | логировать; **не** рвать соединение (`processIncomingEvents` продолжает чтение) |
| Остановлен | ошибка `room stopped` / `peer stopped` | фатально для peer loop (кроме уже остановленного) |
| Прочая ошибка | иная ошибка `Deliver` | `processIncomingEvents` логирует, закрывает `connection` и выходит — ускоренный cleanup полумёртвого peer'а |

`Room.Send`: `ErrQueueFull` у отдельного peer — `Warn` и broadcast продолжается остальным (full-state/patch не падают целиком из-за одного переполненного outbound). Любая другая ошибка `Deliver` (в т.ч. `peer stopped`) **обрывает рассылку** и возвращается вызывающему — до части peers кадр может не дойти.

## Shutdown

`Room.Stop()` завершает event loop через отмену контекста комнаты (`cancel`); `processRoomEvents` выходит по `<-ctx.Done()`.

Каналы `incoming` и `lifecycle` **намеренно не закрываются**: в них пишут внешние горутины (peers, use cases), не входящие в `Room.wg`. Закрытие после `wg.Wait()` приводило к панике `send on closed channel`.

После cancel продюсеры (`Deliver`, `dispatchLifecycle`) возвращают ошибку «room stopped» через `ctx.Err()`, не блокируясь и не паникуя.

Тот же контракт у `peer.outbound`: канал никогда не закрывается, consumer выходит по `ctx.Done()` — конвенция [ADR 0003](../../../docs/adr/0003-channel-ownership.md).

Тесты: `tests/adapter/realtime/room_stop_test.go`, `tests/adapter/registry/room_registry_stop_test.go` (concurrent `Deliver`/`Join`/`Leave` + `Stop`).

## Peer

- `NickName` задаётся в `NewPeer` из ticket (`JoinRoom` → `claims.NickName`)
- I/O через `transport.Connection`
- `outbound` — буфер `PeerOutboundQueueSize` (default 128); полный → `domain.ErrQueueFull` (room loop не стопорится)
- `Ping()` для refresh в policy patch tick
- `IsAlive()` — делегирует в `transport.Connection`; `false` → peer подлежит idle eviction ([ADR 0001](../../../docs/adr/0001-idle-peer-eviction.md))

## Policy

Контракт: `port/realtime/policy.RoomPolicy`.  
Реализация: [policy/state/README.md](policy/state/README.md).

## Codec

Wire encode/decode: [codec/README.md](codec/README.md).

## In-room errors

`room_in_room_error.go` — при ошибке `OnMessage` отправителю кадр OpCode (`domain.InRoomErrorOpCode`).
