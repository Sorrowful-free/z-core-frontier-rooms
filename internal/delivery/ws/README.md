# delivery/ws

WebSocket data plane на том же `*fiber.App`, что HTTP.

## Endpoint

`GET /ws?token=<base64url>`

Ticket по-прежнему в query (контракт клиента). Строка **base64url** из `POST /rooms/:id/tickets` (без padding), URL-encoded в query. Сервер декодирует в сырые байты ticket v3 (v2 тоже принимается) перед `JoinRoomUseCase`. Access-лог Fiber (`fibzap`) пишет `path` без query, чтобы token не попадал в логи.

Клиентская спека: [docs/client-protocol/realtime.md](../../../docs/client-protocol/realtime.md) (обзор), [docs/client-protocol/enet.md](../../../docs/client-protocol/enet.md) (основной транспорт).

1. Декодировать base64url из query → сырые байты ticket (v3/v2)
2. `JoinRoomUseCase`
3. Цикл `connection.Receive` → `room.Deliver`
4. При disconnect → `LeaveRoom`

Ошибка join: один кадр OpCode (`delivery/errors.SendJoinReject`) → close WS.

Нет `token` в query — reject-кадр `OpEmptyToken` (`0x40`) → close; битый base64url — `OpInvalidToken` (`0x41`); token длиннее `MaxIncomingFrameBytes` — close **без** кадра (паритет с oversize-пакетом ENet).

Каждый **успешно отправленный** reject-кадр считается метрикой `rooms_join_rejects_total{opcode}` (P19b: `SendJoinReject` возвращает ошибку send; `sendJoinReject` эмитит `JoinRejected(op)` только при nil, ошибка - Warn-лог).

Idle peer'ы выводятся sweep'ом (`EvictIdlePeersUseCase`), когда соединение перестаёт подтверждать живость: входящие кадры и pong двигают `lastSeen`, `Send` — нет; мёртвый транспорт → `IsAlive() = false` → молчаливый `Close` через `LeaveRoom` (ADR 0001).

Импорт ошибок: `deliveryerrors ".../delivery/errors"`.
