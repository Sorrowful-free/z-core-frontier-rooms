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

**Переполнение:** `room.incoming`, `peer.outbound` и ENet session — **drop** кадра + warn (room loop / host loop / peer read не блокируются).

## Wire

Разбор входящего кадра — `domain.DecodeIncomingFrame` (`OpCode` + payload).  
WS: `SetReadLimit` + decode в `adapter/transport/ws`.  
ENet: проверка в `delivery/enet` до admit и в `frameFromPacket`.

При превышении лимита — **disconnect** без wire-ошибки.
