# domain

Types and sentinel errors with no infrastructure dependencies.

## Key types

- `RoomID`, `PeerID`, `Frame`, `OpCode`
- `RoomAttributes` — arbitrary JSON dictionary of room arguments (`room_attributes.go`)
- `MaxRoomCapacity` (127) — upper bound of room capacity (wire/state: `int8`)
- `DecodeIncomingFrame` — data plane frame parsing (`frame_wire.go`)
- `Claims` - `RoomID`, `PeerID`, `NickName`, `HostID`, `IssuedAt`, `ExpiresAt`
- `HostID` (`u16`) - identifier of the process's ENet host/shard (index+1 in `EnetConfig.ListenPorts`); `HostIDUnbound` (`0`) - ticket not bound to a host (P18, WS-only deploy; rejected with `ErrWrongHost` on ENet-admit)
- `events` — `RoomEvent`, `PeerEvent`

## NickName

`nickname.go`: `ValidateNickName`, `MaxNickNameLen` (64), `ErrInvalidNickName`.  
Required at Issue ticket; lands in `Claims` and then in `Peer`.

## Errors

| File | Purpose |
|------|---------|
| `join_errors.go` | Join/admit (`ErrJoinDenied`, `ErrTicketSlotHeld`, `ErrWrongHost`, …) |
| `frame_wire.go` | Data plane (`ErrIncomingFrameTooLarge`) |
| `room_errors.go` | In-room (`ErrNotMaster`, …), control (`ErrRoomsLimitReached`, …), `ErrQueueFull` |
| `reservation_error.go` | Slots (`ErrReservationFull`, …) |
| `reservation_slot.go` | `ReservationSlot` (enum none/reserved/admitted), `AdmittedSlot` (lastSeen slot for resume/sweep), `RoomSlotDetail` (P31 — read-only slot of any state: `PeerID`, `State`, `ExpiresAt`, `LastSeen`; `reservation.ListRoomSlots` listing for empty-room sweep) |

Wire OpCode: `opcodes.go` (join `0x40+`, in-room `0x60+`, reservation `0x70+`, room notifications `0x80+` — `OpPeerDisconnected`/`OpPeerReconnected`, payload `peerID u64 BE`, P20; `OpRoomClosed` `0x82`, empty payload + disconnect, P31/P33).  
Lifecycle sentinels: `ErrPeerStale` — leave arrived with a peer object displaced by `Replace`/resume (P20, stale teardown); `ErrRoomShouldClose` — policy asks to close the room (P33, no-migration deploy: master left while `allowMasterMigration=false`), internal-only, turned into teardown by usecase.  
In-room mapping: `InRoomErrorOpCode` in `room_errors.go` (called from adapter/realtime and delivery/errors).  
Join/reservation mapping and `Send*` — `internal/delivery/errors`.

## Game state

Model: [state/README.md](state/README.md).  
Game traffic OpCodes: `state/opcodes.go` (`0x01`–`0x07`).
