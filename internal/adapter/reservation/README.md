# adapter/reservation

In-memory room slots. Port: `port/reservation`.

## Locking

- `roomsMu` (`sync.RWMutex`) — only the `roomID → *reservationRoom` map (register / unregister / lookup).
- `reservationRoom.mu` — slots and password of a single room (`Reserve`, `Admit`, `Revoke`, `State`, `ListAdmittedPeers`, `VerifyRoomPassword`).

Operations in different rooms do not serialize each other.

## Methods

| Method | Called by |
|--------|-----------|
| `RegisterRoom` / `UnregisterRoom` | Create / Delete |
| `Reserve` | IssueTicket |
| `Admit` | JoinRoom |
| `Revoke` | LeaveRoom, Issue/Join rollback, orphan cleanup in Issue, sweep |
| `MarkDisconnected` | DisconnectPeer (start of grace, P20) |
| `SlotDetails` | ReconnectRoom (read-only lastSeen for the deadline gate, P24) |
| `Reconnect` | ReconnectRoom (atomic check + lastSeen refresh, P20) |
| `State` | IssueTicket (orphan) |
| `ListAdmittedPeers` | SweepOrphanAdmitted, `closeRoomIfEmpty` (P31) |
| `ListRoomSlots` | SweepEmptyRooms (P31) — read-only listing of slots in **all** states (`domain.RoomSlotDetail`: `PeerID`, `State`, `ExpiresAt`, `LastSeen`); needed by the "neither admitted nor unexpired reserve" predicate |
| `VerifyRoomPassword` | IssueTicket, Delete |

## Slots

`none` → `reserved` (TTL from admission) → `admitted` (until Leave / orphan cleanup / resume).

- `Reserve(ctx, roomID, peerID, nickName, expiresAt)`: one outstanding `reserved` slot per nick in a room — a repeated Reserve with the same nick while the slot is not `admitted`/not removed → `ErrTicketSlotHeld`.
- `Admit` is single-use: a repeated `Admit` on an `admitted` slot → `ErrReservationAlreadyAdmitted`.
- `Revoke` with no peer in the map → `nil` (idempotent).
- Expired `reserved` — lazy sweep on `Reserve`.
- `admitted` stores `lastSeen` — the single slot lifetime timestamp (P20): `MarkDisconnected` updates it at the moment of disconnect (the slot is in grace — capacity is held), `Reconnect` atomically under `room.mu` checks `admitted` + refreshes lastSeen + returns the slot with `NickName` (the sweep-revoke ↔ resume race is closed). Slot missing or not `admitted` → `ErrReservationNotReserved`.
- `SlotDetails` (P24) — the same serialized path as `Reconnect`, but **without** the `lastSeen` refresh: an honest lastSeen for the deadline gate in `ReconnectRoomUseCase` (a rejected resume does not extend the grace window).
- Orphan (no peer in the room) — background sweep (`SweepOrphanAdmittedUseCase`) after `lastSeen + ReconnectTTL` (grace expiry). The same deadline is enforced on resume too: `SlotDetails` + gate in the UC → `ErrReservationExpired` (P24).
- Never-joined/empty rooms (P31) — `SweepEmptyRoomsUseCase` on the same `RESERVATION_ORPHAN_SWEEP_INTERVAL` interval (`0` disables both sweeps): for rooms with `peers==0` it reads `ListRoomSlots` and kills the room when neither admitted nor unexpired reserve remains.
- The room password is stored as SHA-256 (`passwordHash`), compared with `subtle.ConstantTimeCompare`; plaintext is not stored. `RegisterRoom` validates capacity in `[1, domain.MaxRoomCapacity]`.
- Env: `RESERVATION_RECONNECT_TTL` (default `30s`), `RESERVATION_ORPHAN_SWEEP_INTERVAL` (default `10s`; `0` — disable **both** sweeps: orphan admitted and empty rooms, P31).
- `NewReservation(logger, metrics)` — `metrics` (`port/metrics`, nil-safe): slot transition counters `reserve`/`admit`/`revoke` (revoke — any real slot removal, P19b: `Revoke`, expired slot in `Admit`, lazy sweep of expired ones on `Reserve`) and the live slots gauge `reservation_active_slots` (reserve +1, each removal -1, room tombstone on `UnregisterRoom` -len(peers)).

Benchmarks (per-room vs legacy global mutex): `tests/adapter/reservation/reservation_bench_test.go`, see [tests/README.md](../../../tests/README.md).

Errors: `domain/reservation_error.go`. Wire `0x70–0x7F` — `delivery/errors`.
