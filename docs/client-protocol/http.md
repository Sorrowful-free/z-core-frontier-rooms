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

### `POST /rooms` — создать комнату и выдать ticket хосту

**Запрос:**

```json
{
  "capacity": 8,
  "nick_name": "HostPlayer",
  "password": "optional-secret",
  "attributes": {
    "map": "de_dust2",
    "mode": "deathmatch"
  }
}
```

| Поле | Тип | Правила |
|------|-----|---------|
| `capacity` | int | Обязательно, `1…127` (`domain.MaxRoomCapacity`) |
| `nick_name` | string | Обязательно; см. [Правила nick_name](#правила-nick_name) |
| `password` | string | Опционально; если задан — нужен при join через `/tickets` и delete |
| `attributes` | object | Опционально; произвольный JSON-словарь (название карты, режим и т.п.); возвращается в create/list |

ID комнаты выдаёт сервер — в запросе поля `id` нет.

**Успех `201`:**

```json
{
  "token": "AwAAAA...base64url...",
  "reconnect_token": "BAAAAA...base64url...",
  "enet_port": 7777,
  "room": {
    "id": 42,
    "attributes": {
      "map": "de_dust2",
      "mode": "deathmatch"
    },
    "peers": []
  }
}
```

`token` - **base64.RawURLEncoding** (без padding `=`). Декодировать в сырые байты ticket v3 → первый UDP-пакет ([enet.md](enet.md)) или query `?token=` для WS.

`reconnect_token` - **base64.RawURLEncoding**, reconnect-токен v4 (P20): resume под тем же peerID после разрыва в пределах grace-окна `RESERVATION_RECONNECT_TTL`; идёт на тот же admit-вход (`?token=` / первый ENet-пакет). См. [realtime.md](realtime.md#reconnect-p20).

`enet_port` - назначенный UDP-порт ENet host'а (P18, привязка тикета к host'у/shard'у). Поле присутствует, только если на сервере поднят ENet listener.

**Ошибки:** `400` `invalid_capacity`, `400` `invalid_nick_name`, `503` `rooms_limit_reached`, `429` `rate_limited`, а также ошибки issue ticket (`401` `invalid_credentials`, `409` `reservation_full` и т.д.) — при сбое issue созданная комната откатывается.

---

### `GET /rooms` — список комнат

**Успех `200`:**

```json
{
  "rooms": [
    {
      "id": 42,
      "attributes": {
        "map": "de_dust2"
      },
      "peers": [
        {"peer_id": 1, "nick_name": "Alice", "ping": 12}
      ]
    }
  ]
}
```

**Ошибки:** `429` `rate_limited`.

---

### `DELETE /rooms/:id` — удалить комнату

**Запрос** (если у комнаты был password):

```json
{"password": "optional-secret"}
```

**Успех:** `204` без body.

**Ошибки:** `400` `invalid_room_id` (невалидный `:id`), `404` `room_not_found`, `401` `invalid_credentials`, `429` `rate_limited`.

---

### `POST /rooms/:id/tickets` — выдать ticket (joiners)

Для игроков, присоединяющихся к **уже существующей** комнате. Хост получает ticket сразу при create.

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
  "token": "AwAAAA...base64url...",
  "reconnect_token": "BAAAAA...base64url...",
  "enet_port": 7778,
  "room": {
    "id": 42,
    "attributes": {
      "map": "de_dust2"
    },
    "peers": []
  }
}
```

`room` — тот же DTO, что в create/list: `id`, `attributes?`, `peers[]` (текущие peer **в комнате**, без только что зарезервированного слота).

`token` — **base64.RawURLEncoding** (без padding `=`). `reconnect_token` — reconnect-токен v4 (P20) для resume под тем же peerID после разрыва ([realtime.md](realtime.md#reconnect-p20)).

- **ENet (основной):** декодировать в сырые байты ticket v3 → первый UDP-пакет на порт `enet_port` ([enet.md](enet.md)). Ticket привязан к назначенному host'у: admit на другом порту → reject `0x77`.
- **WebSocket (отладка):** строка как есть в query `?token=` ([realtime.md](realtime.md#websocket-отладка)).

**Ошибки:**

| HTTP | `code` | Когда |
|------|--------|-------|
| `400` | `invalid_nick_name` | Пустой nick, control chars, > 64 байт UTF-8 |
| `400` | `invalid_request` | Невалидный JSON body |
| `400` | `invalid_room_id` | Невалидный `:id` в path |
| `401` | `invalid_credentials` | Неверный пароль комнаты |
| `404` | `room_not_found` | Нет комнаты |
| `409` | `ticket_slot_held` | Слот reserved другим peer (TTL не истёк) |
| `409` | `peer_already_in_room` | Peer с этим nick уже в комнате (политика A) |
| `409` | `reservation_full` | Нет свободных слотов |
| `429` | `rate_limited` | Лимит REST endpoint'ов с IP |

## Правила `nick_name`

- Непустая UTF-8 строка
- Длина **в байтах** ≤ 64
- Без control-символов (`codepoint < 0x20`)

## Слоты reservation (кратко)

Issue ticket резервирует слот на TTL (`ADMISSION_TTL`). Пока слот `reserved` или `admitted` без активного peer — повторный issue с другим nick может дать `ticket_slot_held`. Подробнее: [internal/adapter/reservation/README.md](../../internal/adapter/reservation/README.md).

## Godot: пример create room (хост)

```gdscript
var http := HTTPRequest.new()
add_child(http)
var headers := PackedStringArray(["Authorization: Bearer " + api_key, "Content-Type: application/json"])
var body := JSON.stringify({
	"capacity": 8,
	"nick_name": "HostPlayer",
	"password": "",
	"attributes": {"map": "de_dust2"}
})
http.request("http://localhost:3000/rooms", headers, HTTPClient.METHOD_POST, body)
# В ответе JSON: var token: String = parsed["token"], var room_id: int = parsed["room"]["id"]
```

## Godot: пример issue ticket (joiner)

```gdscript
var http := HTTPRequest.new()
add_child(http)
var headers := PackedStringArray(["Authorization: Bearer " + api_key, "Content-Type: application/json"])
var body := JSON.stringify({"nick_name": "Player1", "password": ""})
http.request("http://localhost:3000/rooms/%d/tickets" % room_id, headers, HTTPClient.METHOD_POST, body)
# В ответе JSON: var token: String = parsed["token"]
```

Далее: [enet.md](enet.md) (основной) или [realtime.md](realtime.md#websocket-отладка) (WS smoke-test).
