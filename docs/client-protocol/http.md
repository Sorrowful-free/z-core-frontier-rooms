# HTTP control plane

REST JSON на том же хосте, что WebSocket. Default: `http://localhost:3000` (`HTTP_ADDR`).

Data plane: **ENet UDP** (основной) или WebSocket (отладка) — join только по ticket.

## Аутентификация

Один из заголовков (если `HTTP_AUTH_DISABLED` не `true`):

```http
Authorization: Bearer <HTTP_API_KEY>
```

или

```http
X-API-Key: <HTTP_API_KEY>
```

Без ключа или неверный ключ → `401`:

```json
{"code":"unauthorized","message":"..."}
```

## Endpoints

### `POST /rooms` — создать комнату

**Запрос:**

```json
{
  "capacity": 8,
  "password": "optional-secret"
}
```

| Поле | Тип | Правила |
|------|-----|---------|
| `capacity` | int | Обязательно, `1…127` (`domain.MaxRoomCapacity`) |
| `password` | string | Опционально; если задан — нужен при issue ticket и delete |

Поле `id` в JSON **игнорируется** — ID выдаёт сервер.

**Успех `201`:**

```json
{
  "id": 42,
  "peers": []
}
```

**Ошибки:** `400` `invalid_capacity`, `503` `rooms_limit_reached`, `429` `rate_limited`.

---

### `GET /rooms` — список комнат

**Успех `200`:**

```json
{
  "rooms": [
    {
      "id": 42,
      "peers": [
        {"peer_id": 1, "nick_name": "Alice", "ping": 12}
      ]
    }
  ]
}
```

---

### `DELETE /rooms/:id` — удалить комнату

**Запрос** (если у комнаты был password):

```json
{"password": "optional-secret"}
```

**Успех:** `204` без body.

**Ошибки:** `404` `room_not_found`, `401` `invalid_credentials`.

---

### `POST /rooms/:id/tickets` — выдать ticket

**Запрос:**

```json
{
  "nick_name": "Player1",
  "password": ""
}
```

| Поле | Тип | Правила |
|------|-----|---------|
| `nick_name` | string | Обязательно; см. ниже |
| `password` | string | Пароль комнаты (пустая строка, если комната без пароля) |

**Успех `201`:**

```json
{
  "token": "AgAAAA...base64url..."
}
```

`token` — **base64.RawURLEncoding** (без padding `=`).

- **ENet (основной):** декодировать в сырые байты ticket v2 → первый UDP-пакет ([enet.md](enet.md)).
- **WebSocket (отладка):** строка как есть в query `?token=` ([realtime.md](realtime.md#websocket-отладка)).

**Ошибки:**

| HTTP | `code` | Когда |
|------|--------|-------|
| `400` | `invalid_nick_name` | Пустой nick, control chars, > 64 байт UTF-8 |
| `400` | `invalid_request` | Невалидный JSON / room id |
| `401` | `invalid_credentials` | Неверный пароль комнаты |
| `404` | `room_not_found` | Нет комнаты |
| `409` | `ticket_slot_held` | Слот reserved другим peer (TTL не истёк) |
| `409` | `peer_already_in_room` | Peer с этим nick уже в комнате (политика A) |
| `409` | `reservation_full` | Нет свободных слотов |
| `429` | `rate_limited` | Лимит issue/create с IP |

## Правила `nick_name`

- Непустая UTF-8 строка
- Длина **в байтах** ≤ 64
- Без control-символов (`codepoint < 0x20`)

## Слоты reservation (кратко)

Issue ticket резервирует слот на TTL (`ADMISSION_TTL`). Пока слот `reserved` или `admitted` без активного peer — повторный issue с другим nick может дать `ticket_slot_held`. Подробнее: [internal/adapter/reservation/README.md](../../internal/adapter/reservation/README.md).

## Godot: пример issue ticket

```gdscript
var http := HTTPRequest.new()
add_child(http)
var headers := PackedStringArray(["Authorization: Bearer " + api_key, "Content-Type: application/json"])
var body := JSON.stringify({"nick_name": "Player1", "password": ""})
http.request("http://localhost:3000/rooms/%d/tickets" % room_id, headers, HTTPClient.METHOD_POST, body)
# В ответе JSON: var token: String = parsed["token"]
```

Далее: [enet.md](enet.md) (основной) или [realtime.md](realtime.md#websocket-отладка) (WS smoke-test).
