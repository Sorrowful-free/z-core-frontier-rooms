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

**Переполнение:** `room.incoming`, `peer.outbound` и ENet session — **drop** кадра + warn (room loop / host loop / peer read не блокируются).

## Idle eviction (ADR 0001)

`Connection.IsAlive()` — контракт живости: `false` → peer подлежит idle eviction (`EvictIdlePeersUseCase` выводит через `LeaveRoom`, молчаливый `Close`).

- **WS**: `lastSeen` (UnixNano, atomic) обновляется на успешный входящий кадр и на pong; `Send` не считается. `IsAlive() = false`, если `closed` или `lastSeen` старше `PeerIdleTimeout`. `PeerIdleTimeout == 0` — только `closed` (отрицательные отклоняет `Validate`).
- **ENet**: атомарный флаг, сбрасываемый на `Close()`. Zombie вычищает собственный таймаут ENet (`EventDisconnect` → `LeaveRoom`); флаг — для единого контракта.

## Wire

Разбор входящего кадра — `domain.DecodeIncomingFrame` (`OpCode` + payload).  
WS: `SetReadLimit` + decode в `adapter/transport/ws`.  
ENet: проверка в `delivery/enet` до admit и в `frameFromPacket`.

При превышении лимита — **disconnect** без wire-ошибки.
