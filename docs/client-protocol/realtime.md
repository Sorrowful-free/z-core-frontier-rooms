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

Размер: `67 + len(nick)` … `130` байт.

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
| `0x01`–`0x07` | State / input / RPC — [wire-state-codec.md](../wire-state-codec.md), [state-sync.md](state-sync.md) |
| `0x40`–`0x5F` | Join reject — [errors.md](errors.md) |
| `0x60`–`0x6F` | In-room error (соединение живо) |
| `0x70`–`0x7F` | Reservation reject (при join) |

**Лимит:** `TRANSPORT_MAX_INCOMING_FRAME_BYTES` (env, default 262144).

### Цикл после join

1. `0x01` FullState — инициализация локального state.
2. `0x02` PatchState — merge patches.
3. `0x05`/`0x06` input — любой peer.
4. `0x03`/`0x04` entities — только master.
5. `0x07` RPC — relay по target.

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
| Нет / битый token | Close без кадра |
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

WS ping/pong — транспортный RTT для `Peer.Ping()` на сервере; в игровой протокол не входит.

---

## Дальше

- **ENet (основной):** [enet.md](enet.md)
- State sync: [state-sync.md](state-sync.md)
- Ошибки: [errors.md](errors.md)
