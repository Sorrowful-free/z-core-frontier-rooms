# delivery/ws

WebSocket data plane на том же `*fiber.App`, что HTTP.

## Endpoint

`GET /ws?token=<base64url>`

1. Декодировать token (ticket v2, сырые байты)
2. `JoinRoomUseCase`
3. Цикл `connection.Receive` → `room.Deliver`
4. При disconnect → `LeaveRoom`

Ошибка join: один кадр OpCode (`delivery/errors.SendJoinReject`) → close WS.

Нет `token` в query — close **без** кадра (`0x40` не шлётся).

Импорт ошибок: `deliveryerrors ".../delivery/errors"`.
