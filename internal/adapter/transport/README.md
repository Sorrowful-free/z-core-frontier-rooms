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

Отдельный конфиг ENet listener'а — `enet.EnetConfig` (`enet/enet_config.go`, без build-тегов — нужен и stub-сборкам): `ListenPort`, `PeerLimit`, `ChannelLimit`, `ServiceTimeoutMs`, `AdmitTimeout` + копии `MaxIncomingFrameBytes`/`EnetIncomingQueueSize` из `TransportConfig` (в cmd/rooms). Env `ENET_*` — `adapter/config.LoadFromEnv()` → `cfg.Enet`.

## Idle eviction (ADR 0001)

`Connection.IsAlive()` — контракт живости: `false` → peer подлежит idle eviction (`EvictIdlePeersUseCase` выводит через `LeaveRoom`, молчаливый `Close`).

- **WS**: `lastSeen` (UnixNano, atomic) обновляется на успешный входящий кадр и на pong; `Send` не считается. `IsAlive() = false`, если `closed` или `lastSeen` старше `PeerIdleTimeout`. `PeerIdleTimeout == 0` — только `closed` (отрицательные отклоняет `Validate`).
- **ENet**: атомарный флаг, сбрасываемый на `Close()`. Zombie вычищает собственный таймаут ENet (`EventDisconnect` → `LeaveRoom`); флаг — для единого контракта.

## Wire

Разбор входящего кадра — `domain.DecodeIncomingFrame` (`OpCode` + payload).  
WS: `SetReadLimit` + decode в `adapter/transport/ws`.  
ENet: проверка в `delivery/enet` до admit и в `frameFromPacket`.

При превышении лимита — **disconnect** без wire-ошибки.

## Фабрики соединений — транспортная деталь

Единый seam между delivery и сервисом — `realtime.PeerFactory` (`CreatePeer(ctx, id, nick, transport.Connection, room, logger)`): сервису безразлично, WS или ENet под соединением. `EnetConnectionFactory`/`WsConnectionFactory` остаются в `port/transport/{enet,ws}` ради DI в handler'ах и тестируемости — это деталь конкретного транспорта, а не контракт домена.

У `EnetConnectionFactory` одна untagged-сигнатура `CreateConnection(peer Peer, incoming chan domain.Frame)`: `Peer` — tagged type alias (`port/transport/enet/peer_{cgo,stub}.go`), под `enet && cgo` разворачивается в `libenet.Peer`, иначе — в `any`. Паттерн «tagged alias» убирает расщепление интерфейса по build-тегам без потери типобезопасности cgo-ветки; stub-реализация аргументы игнорирует.

## ENet: сериализация libenet (`Call`/`Do`)

C API libenet не thread-safe; вызовы из host loop (delivery/enet) и peer-горутин (`EnetConnection`) конкурируют. Все вызовы, трогающие C-состояние (`Host`/`Peer`/`Packet`, `NewHost`, `Initialize`/`Deinitialize`), идут через `Call[T]`/`Do` (`enet/connection_cgo.go`) — один `apiMu` на процесс. Внутри fn — ровно один libenet-вызов, без вложенных `Call`/`Do` и без Go-колбэков. `Event`-аксессоры читают Go-копию события — лока не требуют.

**Tradeoff:** `host.Service(ServiceTimeoutMs)` тоже выполняется под `apiMu` — в худшем случае вызовы из peer-горутин ждут ~`ServiceTimeoutMs` (10ms по умолчанию). Ограниченный latency-конвой — принятая цена простой модели сериализации; dispatch вызовов на host-loop горутину — возможный будущий редизайн.

`EnetConnection.Close` шлёт graceful `DisconnectLater` (queued пакеты уходят до разрыва); `Send`/`Ping` после `Close` — `io.EOF`/`-1` по `alive`-флагу.

**go-enet quirk:** `peer.SendBytes`/`SendPacket` игнорируют код возврата `enet_peer_send` — `Send` в disconnecting/zombie peer молча возвращает nil. Разрыв всплывёт позже как `EventDisconnect` → `LeaveRoom`; `alive`-флаг отсекает только явно закрытые conn.
