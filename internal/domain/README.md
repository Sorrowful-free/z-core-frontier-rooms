# domain

Типы и sentinel-ошибки без зависимостей от инфраструктуры.

## Ключевые типы

- `RoomID`, `PeerID`, `Frame`, `OpCode`
- `RoomAttributes` — произвольный JSON-словарь аргументов комнаты (`room_attributes.go`)
- `MaxRoomCapacity` (127) — верхняя граница вместимости комнаты (wire/state: `int8`)
- `DecodeIncomingFrame` — разбор data plane кадра (`frame_wire.go`)
- `Claims` — `RoomID`, `PeerID`, `NickName`, `IssuedAt`, `ExpiresAt`
- `events` — `RoomEvent`, `PeerEvent`

## NickName

`nickname.go`: `ValidateNickName`, `MaxNickNameLen` (64), `ErrInvalidNickName`.  
Обязателен при Issue ticket; попадает в `Claims` и далее в `Peer`.

## Ошибки

| Файл | Назначение |
|------|------------|
| `join_errors.go` | Join/admit (`ErrJoinDenied`, `ErrTicketSlotHeld`, …) |
| `frame_wire.go` | Data plane (`ErrIncomingFrameTooLarge`) |
| `room_errors.go` | In-room (`ErrNotMaster`, …), control (`ErrRoomsLimitReached`, …), `ErrQueueFull` |
| `reservation_error.go` | Слоты (`ErrReservationFull`, …) |

Wire OpCode: `opcodes.go` (join `0x40+`, in-room `0x60+`, reservation `0x70+`).  
Маппинг на wire — только `internal/delivery/errors`.

## Игровой state

Модель: [state/README.md](state/README.md).  
OpCode игрового трафика: `state/opcodes.go` (`0x01`–`0x07`).
