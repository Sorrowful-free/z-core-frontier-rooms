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

`JoinRejectOpCode`, `SendJoinReject` — `join_reject.go`. Reservation-ошибки на join делегируются в `0x70+`; `ErrRoomFull` (`Room.Join` переполнение) мапится в `OpReservationFull` (`0x71`), case выше `ErrJoinDenied` — `ErrRoomFull` может прийти обёрнутым в него (policy `OnJoin`), тогда ordering решает.

`SendJoinReject` / `SendReservationReject` / `SendInRoomError` возвращают ошибку `conn.Send` (P19b): caller считает `join_rejects_total` только при успешной отправке, без неё - логирует. Ошибку можно безопасно игнорировать.

`IsJoinRejectOpCode` покрывает **оба** reject-диапазона (`0x40–0x5F` и `0x70–0x7F`) — оба закрывают соединение. `IsReservationRejectOpCode` — только `0x70–0x7F`, `IsInRoomErrorOpCode` — только `0x60–0x6F`.

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

| OpCode | Hex | Sentinel |
|--------|-----|----------|
| `OpReservationNotFound` | `0x70` | `ErrReservationNotFound` |
| `OpReservationFull` | `0x71` | `ErrReservationFull` |
| `OpReservationSlotHeld` | `0x72` | `ErrTicketSlotHeld` / `ErrReservationAlreadyExists` |
| `OpReservationNotReserved` | `0x73` | `ErrReservationNotReserved` |
| `OpReservationExpired` | `0x74` | `ErrReservationExpired` |
| `OpReservationAlreadyAdmitted` | `0x75` | `ErrReservationAlreadyAdmitted` |
| `OpPeerAlreadyInRoom` | `0x76` | `ErrPeerAlreadyInRoom` |
| `OpWrongHost` | `0x77` | `ErrWrongHost` (strict binding admit, P18) |
| `OpReservationInternal` | `0x7F` | `ErrReservationInternal` |

`OpReservationExpired` (`0x74`) на resume — истёкший grace-дедлайн `lastSeen + RECONNECT_TTL` (P24, гейт в `ReconnectRoomUseCase` до refresh'а); на join — просроченный `reserved`-слот в `Admit`.

## Room notifications (`0x80+`, P20)

Не ошибки и **не через этот пакет**: `OpPeerDisconnected` (`0x80`), `OpPeerReconnected` (`0x81`) рассылают use case'ы (`DisconnectPeer`/`ReconnectRoom`) напрямую через `room.Send` после leave/join — FIFO outbound держит порядок «state-патч → opcode». Payload: `peerID u64 BE` (`domain.NewPeerNotificationFrame`). См. [docs/client-protocol/errors.md](../../../docs/client-protocol/errors.md).

## State traffic (`0x01–0x07`)

Игровой payload — `domain/state/opcodes.go`, layout [docs/wire-state-codec.md](../../../docs/wire-state-codec.md).

Клиентская спека (Godot): [docs/client-protocol/errors.md](../../../docs/client-protocol/errors.md).

Чеклист изменений: [.cursor/rules/join-error-opcodes.mdc](../../../.cursor/rules/join-error-opcodes.mdc).
