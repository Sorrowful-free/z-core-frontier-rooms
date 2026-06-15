# adapter/transport

Лимиты и общая логика data plane (WS + ENet).

## Конфиг

`TransportConfig` — `MaxIncomingFrameBytes` (default **256 KiB**). Env: `TRANSPORT_MAX_INCOMING_FRAME_BYTES`.

## Wire

Разбор входящего кадра — `domain.DecodeIncomingFrame` (`OpCode` + payload).  
WS: `SetReadLimit` + decode в `adapter/transport/ws`.  
ENet: проверка в `delivery/enet` до admit и в `frameFromPacket`.

При превышении лимита — **disconnect** без wire-ошибки.
