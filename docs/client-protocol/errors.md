# Wire errors

Кадр ошибки: **1 байт OpCode**, payload **пустой** (длина кадра = 1).

Импорт на сервере: `deliveryerrors ".../internal/delivery/errors"`.

## Join / admit (`0x40`–`0x5F`)

Соединение **закрывается** после кадра (WS close / ENet disconnect).

| OpCode | Hex | Причина (sentinel) |
|--------|-----|------------------|
| `OpEmptyToken` | `0x40` | Пустой ticket |
| `OpInvalidToken` | `0x41` | `ErrInvalidToken` (HMAC, формат) |
| `OpExpiredToken` | `0x42` | `ErrExpiredToken` |
| `OpRoomNotFound` | `0x43` | `ErrRoomNotFound` |
| `OpJoinDenied` | `0x44` | `ErrJoinDenied` |
| `OpReplaceFailed` | `0x45` | `ErrReplaceFailed` |
| `OpPeerNotFound` | `0x46` | `ErrPeerNotFound` |
| `OpInternal` | `0x50` | default / internal |
| `OpPeerStartFailed` | `0x51` | `ErrPeerStartFailed` |

Reservation-ошибки на join мапятся в диапазон `0x70+` (ниже).

### Реакция Godot (join)

| Hex | Действие клиента |
|-----|------------------|
| `0x40`–`0x42` | Запросить новый ticket (`POST /rooms/:id/tickets`) |
| `0x43` | Комната удалена — вернуться в lobby |
| `0x44`–`0x46`, `0x50`–`0x51` | Показать ошибку, retry с backoff |
| `0x70`–`0x7F` | См. reservation table |

---

## In-room (`0x60`–`0x6F`)

Ошибка **только отправителю**. Соединение **остаётся открытым**.

| OpCode | Hex | Причина |
|--------|-----|---------|
| `OpInRoomInternal` | `0x60` | default |
| `OpInRoomNotMaster` | `0x61` | `ErrNotMaster` — entities не от master |
| `OpInRoomInvalidPayload` | `0x62` | `ErrInRoomInvalidPayload` — битый payload |
| `OpInRoomInvalidRpc` | `0x63` | `ErrInvalidRpcTarget` |
| `OpInRoomRpcPeerNotFound` | `0x64` | `ErrRpcTargetPeerNotFound` |
| `OpInRoomNoMaster` | `0x65` | `ErrNoMaster` — RPC to master, master нет |
| `OpInRoomUnknownOpcode` | `0x66` | `ErrInRoomUnknownOpcode` |

### Реакция Godot (in-room)

| Hex | Действие |
|-----|----------|
| `0x61` | Не слать entities; дождаться смены master |
| `0x62` | Исправить encoder; проверить лимиты map |
| `0x63`–`0x65` | Исправить RPC target / peer_id |
| `0x66` | Неизвестный OpCode — проверить версию протокола |

---

## Reservation (`0x70`–`0x7F`)

При **join** (admit). Payload пустой, соединение закрывается.

| OpCode | Hex | Причина |
|--------|-----|---------|
| `OpReservationNotFound` | `0x70` | `ErrReservationNotFound` |
| `OpReservationFull` | `0x71` | `ErrReservationFull` / `ErrRoomFull` (переполнение `Room.Join`) |
| `OpReservationSlotHeld` | `0x72` | `ErrTicketSlotHeld` / `ErrReservationAlreadyExists` |
| `OpReservationNotReserved` | `0x73` | `ErrReservationNotReserved` |
| `OpReservationExpired` | `0x74` | `ErrReservationExpired` |
| `OpReservationAlreadyAdmitted` | `0x75` | `ErrReservationAlreadyAdmitted` |
| `OpPeerAlreadyInRoom` | `0x76` | `ErrPeerAlreadyInRoom` |
| `OpWrongHost` | `0x77` | `ErrWrongHost` - ticket привязан к другому ENet host'у (P18) |
| `OpReservationInternal` | `0x7F` | `ErrReservationInternal` |

### Реакция Godot (reservation)

| Hex | Действие |
|-----|----------|
| `0x71` | Комната полна |
| `0x72`, `0x76` | Подождать TTL / другой nick / reconnect policy |
| `0x74` | Новый ticket (истёк TTL) |
| `0x75` | Уже admitted — возможно duplicate join |
| `0x77` | Переподключиться на `enet_port` из ответа issue ticket (тот же ticket, слот не потрачен) |

---

## Room notifications (`0x80`+) — не ошибки (P20)

Рассылаются сервером остальным peer'ам комнаты; соединение живо.
Payload: **`peerID u64` big-endian** (8 байт).

| OpCode | Hex | Смысл | Действие клиента |
|--------|-----|-------|------------------|
| `OpPeerDisconnected` | `0x80` | Peer ушёл в reconnect grace (может вернуться под тем же peerID) | Отметить peer как disconnected; state-запись уже удалена (remove-патч шёл раньше по тому же соединению) |
| `OpPeerReconnected` | `0x81` | Peer воскрес под тем же peerID | Отметить peer как live; add-патч/full-state уже был |

Возобновлённый peer **не получает** `0x81` о себе (exclude), но получает `0x01` FullState как обычный join.

## HTTP vs wire

Ошибки **до** join (issue ticket) — JSON на HTTP (`room_not_found`, `ticket_slot_held`, …) — [http.md](http.md).

Ошибки **при** join на **ENet/WS** — wire OpCode (этот документ). ENet: один байт в UDP-пакете перед disconnect — [enet.md](enet.md#admit-первый-пакет).

## Godot: диспетчер OpCode

```gdscript
const OP_FULL_STATE := 0x01
const OP_PATCH_STATE := 0x02
# ... state opcodes ...
const OP_JOIN_REJECT_MIN := 0x40
const OP_IN_ROOM_MIN := 0x60
const OP_RESERVATION_MIN := 0x70

func _handle_frame(opcode: int, payload: PackedByteArray) -> void:
    if opcode >= OP_RESERVATION_MIN:
        _on_join_rejected(opcode)
        ws.close()
    elif opcode >= OP_JOIN_REJECT_MIN:
        _on_join_rejected(opcode)
        ws.close()
    elif opcode >= OP_IN_ROOM_MIN:
        _on_in_room_error(opcode)
    elif opcode == OP_FULL_STATE:
        _apply_full_state(payload)
    elif opcode == OP_PATCH_STATE:
        _apply_patch_state(payload)
    # ...
```

Серверный маппинг: `internal/delivery/errors/join_reject.go`, `reservation_reject.go`.
