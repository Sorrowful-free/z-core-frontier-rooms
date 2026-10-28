# Wire errors

An error frame is **1 byte OpCode** with an **empty payload** (frame length = 1).

Server-side import: `deliveryerrors ".../internal/delivery/errors"`.

## Join / admit (`0x40`–`0x5F`)

The connection is **closed** after the frame (WS close / ENet disconnect).

| OpCode | Hex | Reason (sentinel) |
|--------|-----|-------------------|
| `OpEmptyToken` | `0x40` | Empty ticket |
| `OpInvalidToken` | `0x41` | `ErrInvalidToken` (HMAC, format) |
| `OpExpiredToken` | `0x42` | `ErrExpiredToken` |
| `OpRoomNotFound` | `0x43` | `ErrRoomNotFound` |
| `OpJoinDenied` | `0x44` | `ErrJoinDenied` |
| `OpReplaceFailed` | `0x45` | `ErrReplaceFailed` |
| `OpPeerNotFound` | `0x46` | `ErrPeerNotFound` |
| `OpInternal` | `0x50` | default / internal |
| `OpPeerStartFailed` | `0x51` | `ErrPeerStartFailed` |

Reservation errors during join map to the `0x70+` range (below).

### Godot reaction (join)

| Hex | Client action |
|-----|---------------|
| `0x40`–`0x42` | Request a new ticket (`POST /rooms/:id/tickets`) |
| `0x43` | Room deleted — return to lobby |
| `0x44`–`0x46`, `0x50`–`0x51` | Show error, retry with backoff |
| `0x70`–`0x7F` | See reservation table |

Not every admit failure sends a reject frame — see [Silent closes](#silent-closes) below.

---

## In-room (`0x60`–`0x6F`)

Error goes **only to the sender**. The connection **stays open**.

| OpCode | Hex | Reason |
|--------|-----|--------|
| `OpInRoomInternal` | `0x60` | default |
| `OpInRoomNotMaster` | `0x61` | `ErrNotMaster` — entities from non-master |
| `OpInRoomInvalidPayload` | `0x62` | `ErrInRoomInvalidPayload` — corrupt payload |
| `OpInRoomInvalidRpc` | `0x63` | `ErrInvalidRpcTarget` |
| `OpInRoomRpcPeerNotFound` | `0x64` | `ErrRpcTargetPeerNotFound` |
| `OpInRoomNoMaster` | `0x65` | `ErrNoMaster` — RPC to master, no master |
| `OpInRoomUnknownOpcode` | `0x66` | `ErrInRoomUnknownOpcode` |

### Godot reaction (in-room)

| Hex | Action |
|-----|--------|
| `0x61` | Stop sending entities; wait for master change |
| `0x62` | Fix encoder; check map limits |
| `0x63`–`0x65` | Fix RPC target / peer_id |
| `0x66` | Unknown OpCode — check protocol version |

Transport-level failures (oversized or malformed frame) do **not** take the `0x6x` path — they close the connection silently, see below.

---

## Reservation (`0x70`–`0x7F`)

At **join** (admit). Payload is empty, the connection closes.

| OpCode | Hex | Reason |
|--------|-----|--------|
| `OpReservationNotFound` | `0x70` | `ErrReservationNotFound` |
| `OpReservationFull` | `0x71` | `ErrReservationFull` / `ErrRoomFull` (room `Join` overflow) |
| `OpReservationSlotHeld` | `0x72` | `ErrTicketSlotHeld` / `ErrReservationAlreadyExists` |
| `OpReservationNotReserved` | `0x73` | `ErrReservationNotReserved` |
| `OpReservationExpired` | `0x74` | `ErrReservationExpired` |
| `OpReservationAlreadyAdmitted` | `0x75` | `ErrReservationAlreadyAdmitted` |
| `OpPeerAlreadyInRoom` | `0x76` | `ErrPeerAlreadyInRoom` |
| `OpWrongHost` | `0x77` | `ErrWrongHost` — ticket bound to a different ENet host (P18) |
| `OpReservationInternal` | `0x7F` | `ErrReservationInternal` |

Reachability note: `0x72` is produced only on the HTTP issue-ticket path (JSON error, never a wire frame); on wire, `0x71` is reachable via `ErrRoomFull` when the room is at capacity. `0x7F` is mapped but not emitted by current wire paths — treat it as reserved.

### Godot reaction (reservation)

| Hex | Action |
|-----|--------|
| `0x71` | Room is full |
| `0x72`, `0x76` | Wait for slot TTL / use a different nick / resume with `reconnect_token` |
| `0x74` | New join ticket (slot TTL or resume grace expired) |
| `0x75` | Already admitted — possibly a duplicate join |
| `0x77` | Reconnect to the `enet_port` from the issue-ticket response (same ticket, slot not consumed) |

### Resume-reject (`reconnect_token`)

Resume goes through the same admit entry (`?token=` / first ENet packet) — the reject frames are the same, reasons read for resume:

| Hex | OpCode | Reason on resume |
|-----|--------|------------------|
| `0x41` | `OpInvalidToken` | Corrupt/foreign `reconnect_token` (HMAC, format, kind) |
| `0x43` | `OpRoomNotFound` | Room already deleted — resume impossible, a new join ticket is needed |
| `0x73` | `OpReservationNotReserved` | No slot / not `admitted` (sweep already revoked an expired grace, foreign `peer_id`) |
| `0x74` | `OpReservationExpired` | Grace expired: `lastSeen + RESERVATION_RECONNECT_TTL` has passed. The deadline is checked **before** `lastSeen` refresh — rejected attempts do not extend grace |
| `0x77` | `OpWrongHost` | `host_id` of the reconnect token ≠ the accepting ENet host (inherited strict binding) |

`0x42` `OpExpiredToken` cannot occur on resume — the reconnect token carries no timestamps; grace is tracked by the server on the slot. Other codes from the shared tables (`0x45`, `0x46`, `0x50`–`0x51`, `0x70`, `0x7F`) are possible on races — same mapping.

---

## Room notifications `0x80`+ (not errors)

Broadcast by the server to room peers.

| OpCode | Hex | Meaning | Client action |
|--------|-----|---------|---------------|
| `OpPeerDisconnected` | `0x80` | Peer entered reconnect grace (may return under the same peerID) | Mark peer as disconnected; its remove patch in state arrives on the next patch tick (P34) |
| `OpPeerReconnected` | `0x81` | Peer revived under the same peerID | Mark peer as live; the returner received `0x01` FullState, others learn via the patch tick |
| `OpRoomClosed` | `0x82` | Room closed by the server. Sources (P31): no-migration deploy (`STATE_POLICY_ALLOW_MASTER_MIGRATION=false`, P33) and the host left; `DELETE /rooms/:id`; server shutdown. **Payload empty**, connection **closes** after the frame | Room is gone — exit to lobby; reconnect/new join impossible (`0x43`) |

Payload of `0x80`/`0x81`: **`peerID u64` big-endian** (8 bytes). `0x82` has an empty payload and the server tears the connection down right after the frame — delivery before the teardown is **guaranteed** (P30, drain-aware `peer.Stop`: `Send` before `Close` within the drain timeout; on WS it was best-effort before P30). The handler is mandatory, otherwise the close just looks like a transport failure.

The revived peer does **not** receive `0x81` about itself (excluded) but does get `0x01` FullState like a regular join.

---

## Silent closes

These failures close the connection **without any OpCode frame** — the client sees a bare disconnect:

| Situation | Transport |
|-----------|-----------|
| Token larger than the incoming frame limit | WS |
| First packet larger than `MaxIncomingFrameBytes` | ENet |
| No admit packet within `ENET_ADMIT_TIMEOUT` | ENet |
| Malformed/oversized frame **after** admit | WS + ENet |
| Idle timeout (`TRANSPORT_PEER_IDLE_TIMEOUT`) | WS |

## HTTP vs wire

Errors **before** join (issue ticket) are JSON over HTTP (`room_not_found`, `ticket_slot_held`, …) — [http.md](http.md).

Errors **at** join over **ENet/WS** are wire OpCodes (this document). ENet: one byte in a UDP packet before disconnect — [enet.md](enet.md#admit-first-packet).

## Godot: OpCode dispatcher

```gdscript
const OP_FULL_STATE := 0x01
const OP_PATCH_STATE := 0x02
# ... state opcodes ...
const OP_JOIN_REJECT_MIN := 0x40   # 0x40-0x5F
const OP_IN_ROOM_MIN := 0x60       # 0x60-0x6F
const OP_RESERVATION_MIN := 0x70   # 0x70-0x7F
const OP_NOTIFY_MIN := 0x80        # 0x80+
const OP_ROOM_CLOSED := 0x82

func _handle_frame(opcode: int, payload: PackedByteArray) -> void:
    if opcode >= OP_NOTIFY_MIN:                    # 0x80+ notifications
        _on_room_notification(opcode, payload)
        if opcode == OP_ROOM_CLOSED:
            ws.close()  # server closes anyway
    elif opcode >= OP_RESERVATION_MIN:             # 0x70-0x7F join reject
        _on_join_rejected(opcode)
        ws.close()
    elif opcode >= OP_IN_ROOM_MIN:                 # 0x60-0x6F in-room error
        _on_in_room_error(opcode)                  # connection stays open
    elif opcode >= OP_JOIN_REJECT_MIN:             # 0x40-0x5F join reject
        _on_join_rejected(opcode)
        ws.close()
    elif opcode == OP_FULL_STATE:
        _apply_full_state(payload)
    elif opcode == OP_PATCH_STATE:
        _apply_patch_state(payload)
    # ...
```

The check order matters: bands are contiguous, so test the highest range first (or use explicit `>= MIN && <= MAX` bounds). `0x80+` frames are notifications, not rejects — misrouting them into `_on_join_rejected` + `ws.close()` breaks reconnect handling.

Server-side mapping: `internal/delivery/errors/join_reject.go`, `reservation_reject.go`.
