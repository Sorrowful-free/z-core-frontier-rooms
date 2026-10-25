# Realtime data plane

Общий контракт data plane после [HTTP ticket](http.md).

| Транспорт | Назначение | Документ |
|-----------|------------|----------|
| **ENet UDP** | Основной (Godot) | **[enet.md](enet.md)** |
| WebSocket | Быстрая отладка / smoke-test | ниже |

Ticket v2, формат кадра и игровой цикл после join **одинаковы**; отличается только способ передачи ticket при connect.

---

## Ticket v2 (бинарный)

Клиент **не генерирует** ticket — только получает из HTTP.

```text
version     u8   = 2
room_id     u64 BE
peer_id     u64 BE
issued_at   u64 BE
expires_at  u64 BE
nick_len    u8
nick        UTF-8 bytes (1…64)
hmac        32 bytes SHA-256 HMAC
```

Размер: `66 + len(nick)` … `130` байт.

| Транспорт | Как передать ticket |
|-----------|---------------------|
| ENet | Декодировать base64url → **сырые байты** в первом UDP-пакете |
| WebSocket | Строка base64url в `?token=` (сервер декодирует) |

Подробнее ENet: [enet.md](enet.md). Admission: [internal/adapter/admission/README.md](../../internal/adapter/admission/README.md).

---

## Кадр data plane (общий)

После успешного join (WS или ENet):

```text
[OpCode: u8][Payload: variable]
```

| OpCode | Назначение |
|--------|------------|
| `0x01`–`0x07` | State / input / RPC — [wire-state-codec.md](../wire-state-codec.md), [state-sync.md](state-sync.md). **Breaking (P6d):** `password` удалён из state payload |
| `0x40`–`0x5F` | Join reject — [errors.md](errors.md) |
| `0x60`–`0x6F` | In-room error (соединение живо) |
| `0x70`–`0x7F` | Reservation reject (при join) |
| `0x80`–`0x81` | Room notifications `OpPeerDisconnected`/`OpPeerReconnected` (payload `peerID u64 BE`) — [errors.md](errors.md#room-notifications-0x80-не-ошибки-p20) |

**Лимит:** `TRANSPORT_MAX_INCOMING_FRAME_BYTES` (env, default 262144).

### Цикл после join

1. `0x01` FullState — инициализация локального state.
2. `0x02` PatchState — merge patches.
3. `0x05`/`0x06` input — любой peer.
4. `0x03`/`0x04` entities — только master.
5. `0x07` RPC — relay по target.

---

## Reconnect (P20)

Краткий разрыв транспорта **не** уничтожает идентичность: peer сразу исчезает из комнаты (остальные получают remove-патч и `0x80`), но слот reservation держит capacity и peerID в пределах grace-окна `RECONNECT_TTL` (серверный, default `30s`, от момента разрыва).

1. `POST /rooms/:id/tickets` возвращает `reconnect_token` — сохранить до разрыва (без expiry у токена; живёт, пока жив admitted-слот).
2. Разрыв → сервер рассылает `0x80` остальным.
3. Повторный admit тем же транспортом: WS `?token=<reconnect_token>` или первый ENet-пакет — вместо join-тикета.
4. Успех: peer возвращается под **тем же peerID** и тем же nick'ом (nick из слота — в токене его нет), получает `0x01` FullState как обычный join; остальным — `0x81` с `peerID u64 BE`.
5. Просроченный grace или чужой/битый токен → join-reject (те же коды `0x41`/`0x43`/`0x46`/`0x73`).
6. Новейшее соединение побеждает: reconnect при живом старом conn вытесняет его (Replace).

**Token v4 (reconnect):** `version(1)=4 | roomID u64 | peerID u64 | hostID u16 | HMAC-SHA256(32)` — 51 байт, base64url как join-тикет; без nick и timestamps; `hostID` наследует привязку исходного тикета (strict binding ENet).

---

## WebSocket (отладка)

Используйте WS, чтобы проверить HTTP → ticket → join → state **без** ENet/CGO.

| Параметр | Значение |
|----------|----------|
| URL | `ws://<host>/ws?token=<base64url>` |
| Token | Строка из JSON `POST /tickets` как есть, `uri_encode` в URL |
| Сообщения | Binary WS frames only |
| API key | Не нужен |

| Исход join | Поведение |
|------------|-----------|
| Нет token | `0x40` OpEmptyToken → close |
| Битый base64url token | `0x41` OpInvalidToken → close |
| Token длиннее лимита | Close без кадра (как oversize-пакет в ENet) |
| Ошибка join | 1 байт `0x40+` → close |
| Успех | Первый кадр `0x01` FullState |

WS **не** поддерживает reliable/unreliable — всё implicit reliable. Для продакшн-клиента см. [enet.md](enet.md).

### Godot: минимальный smoke-test

```gdscript
var ws := WebSocketPeer.new()
ws.connect_to_url("ws://localhost:3000/ws?token=%s" % token.uri_encode())

func _process(_delta):
    ws.poll()
    while ws.get_available_packet_count() > 0:
        var packet := ws.get_packet()
        _handle_frame(packet[0], packet.slice(1))

func send_frame(opcode: int, payload: PackedByteArray) -> void:
    var frame := PackedByteArray([opcode])
    frame.append_array(payload)
    ws.send(frame)
```

WS ping/pong — транспортный RTT для `Peer.Ping()` на сервере; в игровой протокол не входит. Pong также подтверждает живость: если ни кадры, ни pong не приходят дольше `TRANSPORT_PEER_IDLE_TIMEOUT` (default 30s), сервер молча закрывает соединение (idle eviction, без опкода).

---

## Дальше

- **ENet (основной):** [enet.md](enet.md)
- State sync: [state-sync.md](state-sync.md)
- Ошибки: [errors.md](errors.md)
