# domain

Типы и sentinel-ошибки без зависимостей от инфраструктуры.

## Ключевые типы

- `RoomID`, `PeerID`, `Frame`, `OpCode`
- `RoomAttributes` — произвольный JSON-словарь аргументов комнаты (`room_attributes.go`)
- `MaxRoomCapacity` (127) — верхняя граница вместимости комнаты (wire/state: `int8`)
- `DecodeIncomingFrame` — разбор data plane кадра (`frame_wire.go`)
- `Claims` - `RoomID`, `PeerID`, `NickName`, `HostID`, `IssuedAt`, `ExpiresAt`
- `HostID` (`u16`) - идентификатор ENet host'а/shard'а процесса (индекс+1 в `EnetConfig.ListenPorts`); `HostIDAny` (`0`) - ticket без привязки к host'у (P18)
- `events` — `RoomEvent`, `PeerEvent`

## NickName

`nickname.go`: `ValidateNickName`, `MaxNickNameLen` (64), `ErrInvalidNickName`.  
Обязателен при Issue ticket; попадает в `Claims` и далее в `Peer`.

## Ошибки

| Файл | Назначение |
|------|------------|
| `join_errors.go` | Join/admit (`ErrJoinDenied`, `ErrTicketSlotHeld`, `ErrWrongHost`, …) |
| `frame_wire.go` | Data plane (`ErrIncomingFrameTooLarge`) |
| `room_errors.go` | In-room (`ErrNotMaster`, …), control (`ErrRoomsLimitReached`, …), `ErrQueueFull` |
| `reservation_error.go` | Слоты (`ErrReservationFull`, …) |

Wire OpCode: `opcodes.go` (join `0x40+`, in-room `0x60+`, reservation `0x70+`, room notifications `0x80+` — `OpPeerDisconnected`/`OpPeerReconnected`, payload `peerID u64 BE`, P20).  
Sentinels lifecycle: `ErrPeerStale` — leave пришёл с peer-объектом, вытесненным `Replace`/resume (P20, stale teardown).  
In-room маппинг: `InRoomErrorOpCode` в `room_errors.go` (вызывается из adapter/realtime и delivery/errors).  
Join/reservation маппинг и `Send*` — `internal/delivery/errors`.

## Игровой state

Модель: [state/README.md](state/README.md).  
OpCode игрового трафика: `state/opcodes.go` (`0x01`–`0x07`).
