# adapter/transport

Лимиты и общая логика data plane (WS + ENet).

## Конфиг

`TransportConfig`:

| Поле | Default | Env |
|------|---------|-----|
| `MaxIncomingFrameBytes` | 256 KiB | `TRANSPORT_MAX_INCOMING_FRAME_BYTES` |
| `RoomIncomingQueueSize` | 128 | `TRANSPORT_ROOM_INCOMING_QUEUE` (`0` = unbuffered) |
| `PeerOutboundQueueSize` | 128 | `TRANSPORT_PEER_OUTBOUND_QUEUE` |
| `EnetIncomingQueueSize` | 256 | `TRANSPORT_ENET_INCOMING_QUEUE` (полный → **drop**, host loop не блокируется) |
| `PeerIdleTimeout` | 30s | `TRANSPORT_PEER_IDLE_TIMEOUT` (`0` — механизм отключён; отрицательные — ошибка Validate) |
| `PeerIdleSweepInterval` | 10s | `TRANSPORT_PEER_IDLE_SWEEP_INTERVAL` (`0` — sweep не стартует; отрицательные — ошибка Validate) |
| `WSWriteDeadline` | 10s | `TRANSPORT_WS_WRITE_DEADLINE` (`0` — без дедлайна; отрицательные — ошибка Validate) |

`MaxIncomingFrameBytes=0` — **без лимита** входящего кадра (опасно, явный opt-in; см. `adapter/config/README.md`).

**Переполнение:** `room.incoming`, `peer.outbound` и ENet session — **drop** кадра + warn (room loop / host loop / peer read не блокируются).

Отдельный конфиг ENet listener'а - `enet.EnetConfig` (`enet/enet_config.go`, без build-тегов - нужен и stub-сборкам): `ListenPorts` (1..N UDP-портов host-shard'ов, P18), `PeerLimit` (per-host), `ChannelLimit`, `ServiceTimeoutMs`, `AdmitTimeout` + копии `MaxIncomingFrameBytes`/`EnetIncomingQueueSize` из `TransportConfig` (в cmd/rooms). Env `ENET_*` - `adapter/config.LoadFromEnv()` → `cfg.Enet`.

## Idle eviction (ADR 0001)

`Connection.IsAlive()` — контракт живости: `false` → peer подлежит idle eviction (`EvictIdlePeersUseCase` выводит через `DisconnectPeer`, молчаливый `Close`).

- **WS**: `lastSeen` (UnixNano, atomic) обновляется на успешный входящий кадр и на pong; `Send` не считается. `IsAlive() = false`, если `closed` или `lastSeen` старше `PeerIdleTimeout`. `PeerIdleTimeout == 0` — только `closed` (отрицательные отклоняет `Validate`).
- **ENet**: атомарный флаг, сбрасываемый на `Close()`. Zombie вычищает собственный таймаут ENet (`EventDisconnect` → `DisconnectPeer`); флаг — для единого контракта.

## Wire

Разбор входящего кадра — `domain.DecodeIncomingFrame` (`OpCode` + payload).  
WS: `SetReadLimit` + decode в `adapter/transport/ws`.  
ENet: проверка в `delivery/enet` до admit и в `frameFromPacket`.

При превышении лимита — **disconnect** без wire-ошибки.

## Фабрики соединений — транспортная деталь

Единый seam между delivery и сервисом — `realtime.PeerFactory` (`CreatePeer(ctx, id, nick, transport.Connection, room, logger)`): сервису безразлично, WS или ENet под соединением. `EnetConnectionFactory`/`WsConnectionFactory` остаются в `port/transport/{enet,ws}` ради DI в handler'ах и тестируемости — это деталь конкретного транспорта, а не контракт домена.

У `EnetConnectionFactory` одна untagged-сигнатура `CreateConnection(peer Peer, incoming chan domain.Frame)`: `Peer` — tagged type alias (`port/transport/enet/peer_{cgo,stub}.go`), под `enet && cgo` разворачивается в `libenet.Peer`, иначе — в `any`. Паттерн «tagged alias» убирает расщепление интерфейса по build-тегам без потери типобезопасности cgo-ветки; stub-реализация аргументы игнорирует.

## ENet: сериализация libenet (`HostLoop`)

C API libenet не thread-safe; host, его peers и packets трогает только goroutine `HostLoop` (`enet/hostloop_cgo.go`, P15) - executor, разбирающий FIFO-очередь команд. `NewHostLoop` создаёт host внутри себя: владельцы (server `delivery/enet` run, тестовые клиенты) гоняют работу blocking-командами `Call[T](loop, fn)`/`loop.Do(fn)` - включая `host.Service` как команду (клиентский pump двигает свой service сам); `EnetConnection` шлёт `Send`/`Close` async через `loop.enqueue`, `Ping` - blocking `Call`. Глобальной сериализации нет - конкурируют только submit'ы одного host'а, худший latency send по-прежнему ~`ServiceTimeoutMs`, зато конвой между host'ами ушёл.

Инвариант живучести: executor исполняет только ограниченные по времени libenet-вызовы и никогда не ждёт доменных горутин - blocking `Call`/`Do` поэтому deadlock'нуться не может. Доменная работа, способная блокироваться (admit/teardown → `AdmitPeer`/`DisconnectPeer` → `dispatchLifecycle` → ожидание room-горутины), выполняется вне команд loop'а (delivery dispatch идёт на run-горутине); иначе `Ping` из room-контекста замкнул бы cross-deadlock executor↔room. `Event`-аксессоры читают Go-копию события - confinement'а не требуют. `loop.Stop()` закрывает приём blocking-команд → drain очереди → `host.Destroy`; submit'ы после stop получают `ErrHostLoopStopped`, ожидающие caller'ы разблокируются исполнением или ошибкой.

`EnetConnection.Close` кладёт graceful `DisconnectLater` в очередь loop'а (FIFO сохраняет wire-порядок queued пакетов до разрыва); `Send`/`Ping` после `Close` - `io.EOF`/`-1` по `alive`-флагу.

**go-enet quirk:** `peer.SendBytes`/`SendPacket` игнорируют код возврата `enet_peer_send` — `Send` в disconnecting/zombie peer молча возвращает nil. Разрыв всплывёт позже как `EventDisconnect` → `DisconnectPeer`; `alive`-флаг отсекает только явно закрытые conn.
