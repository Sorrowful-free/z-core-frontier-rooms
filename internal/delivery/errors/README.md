# delivery/errors

Mapping of `domain` errors → wire OpCode. Frame: **1 byte OpCode + empty payload** (for reject/error).

Import: `deliveryerrors "github.com/.../internal/delivery/errors"`.

## Join / admit (`0x40–0x5F`)

| OpCode | Hex | Sentinel |
|--------|-----|----------|
| `OpEmptyToken` | `0x40` | empty ticket |
| `OpInvalidToken` | `0x41` | `ErrInvalidToken` |
| `OpExpiredToken` | `0x42` | `ErrExpiredToken` |
| `OpRoomNotFound` | `0x43` | `ErrRoomNotFound` |
| `OpJoinDenied` | `0x44` | `ErrJoinDenied` |
| `OpReplaceFailed` | `0x45` | `ErrReplaceFailed` |
| `OpPeerNotFound` | `0x46` | `ErrPeerNotFound` |
| `OpInternal` | `0x50` | default |
| `OpPeerStartFailed` | `0x51` | `ErrPeerStartFailed` |

`JoinRejectOpCode`, `SendJoinReject` — `join_reject.go`. Reservation errors on join are delegated to `0x70+`; `ErrRoomFull` (`Room.Join` overflow) maps to `OpReservationFull` (`0x71`), the case sits above `ErrJoinDenied` — `ErrRoomFull` may arrive wrapped in it (policy `OnJoin`), in which case ordering decides.

`SendJoinReject` / `SendReservationReject` / `SendInRoomError` return the `conn.Send` error (P19b): the caller counts `join_rejects_total` only on a successful send, otherwise it logs. The error can be safely ignored.

`IsJoinRejectOpCode` covers **both** reject ranges (`0x40–0x5F` and `0x70–0x7F`) — both close the connection. `IsReservationRejectOpCode` — only `0x70–0x7F`, `IsInRoomErrorOpCode` — only `0x60–0x6F`.

## In-room (`0x60–0x6F`)

Error **to the sender**; the connection stays open.

| OpCode | Hex | Sentinel |
|--------|-----|----------|
| `OpInRoomInternal` | `0x60` | default |
| `OpInRoomNotMaster` | `0x61` | `ErrNotMaster` |
| `OpInRoomInvalidPayload` | `0x62` | `ErrInRoomInvalidPayload` |
| `OpInRoomInvalidRpc` | `0x63` | `ErrInvalidRpcTarget` |
| `OpInRoomRpcPeerNotFound` | `0x64` | `ErrRpcTargetPeerNotFound` |
| `OpInRoomNoMaster` | `0x65` | `ErrNoMaster` |
| `OpInRoomUnknownOpcode` | `0x66` | `ErrInRoomUnknownOpcode` |

Mapping: `domain.InRoomErrorOpCode` (`room_errors.go`); this package holds the wrapper + `SendInRoomError` (`join_reject.go`).

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

`OpReservationExpired` (`0x74`) on resume — expired grace deadline `lastSeen + RESERVATION_RECONNECT_TTL` (P24, gate in `ReconnectRoomUseCase` before the refresh); on join — an overdue `reserved` slot in `Admit`.

`ErrPeerStale` (identity-scoped teardown in the peer registry, P20) — **internal-only** sentinel: absent from the tables and unreachable on the wire (it would map to `OpInternal`); it has no dedicated opcode.

## Room notifications (`0x80+`, P20)

Not errors and **not via this package**: `OpPeerDisconnected` (`0x80`), `OpPeerReconnected` (`0x81`) are broadcast by use cases (`DisconnectPeer`/`ReconnectRoom`) directly through `room.Send` after leave/join — the outbound FIFO preserves "state patch → opcode" order. Payload: `peerID u64 BE` (`domain.NewPeerNotificationFrame`). See [docs/client-protocol/errors.md](../../../docs/client-protocol/errors.md).

## State traffic (`0x01–0x07`)

Game payload — `domain/state/opcodes.go`, layout [docs/wire-state-codec.md](../../../docs/wire-state-codec.md).

Client spec (Godot): [docs/client-protocol/errors.md](../../../docs/client-protocol/errors.md).

Change checklist: [.cursor/rules/join-error-opcodes.mdc](../../../.cursor/rules/join-error-opcodes.mdc).
