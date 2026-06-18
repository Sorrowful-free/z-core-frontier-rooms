# delivery/ws

WebSocket data plane на том же `*fiber.App`, что HTTP.

## Endpoint

`GET /ws?token=<base64url>`

Ticket по-прежнему в query (контракт клиента). Строка **base64url** из `POST /rooms/:id/tickets` (без padding), URL-encoded в query. Сервер декодирует в сырые байты ticket v2 перед `JoinRoomUseCase`. Access-лог Fiber (`fibzap`) пишет `path` без query, чтобы token не попадал в логи.

Клиентская спека: [docs/client-protocol/realtime.md](../../../docs/client-protocol/realtime.md) (обзор), [docs/client-protocol/enet.md](../../../docs/client-protocol/enet.md) (основной транспорт).

1. Декодировать base64url из query → ticket v2 (сырые байты)
2. `JoinRoomUseCase`
3. Цикл `connection.Receive` → `room.Deliver`
4. При disconnect → `LeaveRoom`

Ошибка join: один кадр OpCode (`delivery/errors.SendJoinReject`) → close WS.

Нет `token` в query — close **без** кадра (`0x40` не шлётся).

Импорт ошибок: `deliveryerrors ".../delivery/errors"`.
