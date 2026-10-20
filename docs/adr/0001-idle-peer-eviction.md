# Idle peer eviction через контракт `IsAlive()` и периодический sweep

WS-`Receive()` блокируется без read deadline: при полуоткрытом TCP (клиент умер без close-фрейма) peer висит в комнате до TCP-таймаута. `Ping()` возвращает только последнее значение RTT — у zombie оно устаревает, но отличить stale от свежего нельзя. Решено: транспортно-агностичный контракт `IsAlive()` на `transport.Connection` и `realtime.Peer`; периодический `EvictIdlePeersUseCase` (по образцу orphan admitted sweep) выводит `!IsAlive()` peer'ов через `LeaveRoomUseCase`. Eviction — молчаливый `Close` + warn-лог, новый wire-OpCode для кика не вводится.

WS-реализация: внутренний `lastSeen` (входящие кадры + pong, `Send` не считается) против `PeerIdleTimeout` из `TransportConfig`. ENet-реализация: атомарный флаг, сбрасываемый на `Close()`/disconnect — проверка `codecat/go-enet` показала, что наружу экспонируются только RTT-метрики, протокольная живость не наблюдаема; zombie на ENet и так вычищает собственный таймаут транспорта (`EventDisconnect` → `LeaveRoom`).

## Considered Options

- **`LastSeen() time.Time` на интерфейсах** — отклонено: семантика таймаута утекает в sweep, а для ENet не решает проблему «молчаливый живой клиент» (протокольные ping'и не доходят до `Receive`, LastSeen не двигается → ложный eviction).
- **Per-peer watchdog в `Peer`** — отклонено: горутина на peer, не по образцу существующих sweep-механизмов.
- **Tick в `Room`/`RoomPolicy`** — отклонено: у policy нет доступа к reservation, полный leave-поток (`Revoke`) не собрать.

## Consequences

- Порог idle для WS живёт внутри `WsConnection` — sweep ничего не знает о таймауте, только о решении `IsAlive()`.
- `PeerIdleTimeout <= 0` выключает механизм: sweep не стартует (`cmd/rooms` гейтит запуск), а `IsAlive()` при выключенном таймауте отражает только факт `Close()` (closed → `false`).
- ENet-флаг не добавляет защиты сверх ENet-таймаута, но сохраняет единый контракт без type-switch.
