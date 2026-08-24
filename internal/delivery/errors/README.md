# delivery/errors

Маппинг `domain` ошибок → wire OpCode. Кадр: **1 байт OpCode + пустой payload** (для reject/error).

Импорт: `deliveryerrors "github.com/.../internal/delivery/errors"`.

## Join / admit (`0x40–0x5F`)

| OpCode | Hex | Sentinel |
|--------|-----|----------|
| `OpEmptyToken` | `0x40` | пустой ticket |
| `OpInvalidToken` | `0x41` | `ErrInvalidToken` |
| `OpExpiredToken` | `0x42` | `ErrExpiredToken` |
| `OpRoomNotFound` | `0x43` | `ErrRoomNotFound` |
| `OpJoinDenied` | `0x44` | `ErrJoinDenied` |
| `OpReplaceFailed` | `0x45` | `ErrReplaceFailed` |
| `OpPeerNotFound` | `0x46` | `ErrPeerNotFound` |
| `OpInternal` | `0x50` | default |
| `OpPeerStartFailed` | `0x51` | `ErrPeerStartFailed` |

`JoinRejectOpCode`, `SendJoinReject` — `join_reject.go`. Reservation-ошибки на join делегируются в `0x70+`.

## In-room (`0x60–0x6F`)

Ошибка **отправителю**; соединение остаётся открытым.

| OpCode | Hex | Sentinel |
|--------|-----|----------|
| `OpInRoomInternal` | `0x60` | default |
| `OpInRoomNotMaster` | `0x61` | `ErrNotMaster` |
| `OpInRoomInvalidPayload` | `0x62` | `ErrInRoomInvalidPayload` |
| `OpInRoomInvalidRpc` | `0x63` | `ErrInvalidRpcTarget` |
| `OpInRoomRpcPeerNotFound` | `0x64` | `ErrRpcTargetPeerNotFound` |
| `OpInRoomNoMaster` | `0x65` | `ErrNoMaster` |
| `OpInRoomUnknownOpcode` | `0x66` | `ErrInRoomUnknownOpcode` |

Маппинг: `domain.InRoomErrorOpCode` (`room_errors.go`); в этом пакете — обёртка + `SendInRoomError` (`join_reject.go`).

## Reservation (`0x70–0x7F`)

`ReservationRejectOpCode`, `SendReservationReject` — `reservation_reject.go`.

| OpCode | Hex |
|--------|-----|
| `OpReservationNotFound` | `0x70` |
| `OpReservationFull` | `0x71` |
| `OpReservationSlotHeld` | `0x72` |
| `OpReservationNotReserved` | `0x73` |
| `OpReservationExpired` | `0x74` |
| `OpReservationAlreadyAdmitted` | `0x75` |
| `OpPeerAlreadyInRoom` | `0x76` |
| `OpReservationInternal` | `0x7F` |

## State traffic (`0x01–0x07`)

Игровой payload — `domain/state/opcodes.go`, layout [docs/wire-state-codec.md](../../../docs/wire-state-codec.md).

Клиентская спека (Godot): [docs/client-protocol/errors.md](../../../docs/client-protocol/errors.md).

Чеклист изменений: [.cursor/rules/join-error-opcodes.mdc](../../../.cursor/rules/join-error-opcodes.mdc).
