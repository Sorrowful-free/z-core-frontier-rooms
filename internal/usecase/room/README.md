# usecase/room

Room scenarios. Depends on `port/*`, not on `delivery` and not on concrete adapters.

## Use cases

| File | Plane | Summary |
|------|-------|---------|
| `create.go` | Control | `limits.AllowCreateRoom` → `RegisterRoom` → registry → `IssueTicket` (host); room rollback on issue error |
| `delete.go` | Control | DELETE/shutdown — same `closeRoom`: `OpRoomClosed` (0x82) to all → `DeleteRoom` → `UnregisterRoom` → parallel `Stop` |
| `get_list.go` | Control | Room list |
| `issue_ticket.go` | Control | `ValidateNickName` → AssignHost (ENet sharding) → Reserve → Issue + IssueReconnect → room snapshot (`RoomSummary`) + `EnetPort` + reconnect token |
| `admit_peer.go` | Data | Facade (P20): Validate once → claims.Kind → join or resume; ENet strict binding (enetHostID) |
| `join_room.go` | Data | Admit → CreatePeer(nick) → Join/Replace → Start → `PeerSession` |
| `reconnect_room.go` | Data | Resume (P20/P24): `reservation.SlotDetails` → deadline gate (`ErrReservationExpired`) → `reservation.Reconnect` → CreatePeer(nick from slot) → Join/Replace → Start → `OpPeerReconnected` |
| `disconnect_peer.go` | Data | Disconnect into grace (P20): `GetPeer` identity-check → `MarkDisconnected` → Leave → Stop → `OpPeerDisconnected` |
| `leave_room.go` | Data | Leave → Stop → Revoke (explicit leave / rollback); `ErrRoomShouldClose` → `closeRoom` |
| `close_room.go` | Data | `closeRoom` — teardown on policy request (P29) and DELETE/shutdown (P31): `OpRoomClosed` to remaining → `DeleteRoom` → `UnregisterRoom` → `Stop` all. `closeRoomIfEmpty` — death of an empty room without broadcast (no one to send to) |
| `peer_session.go` | Data | `PeerSession{RoomID, PeerID, Peer}` — session handle until teardown (P20) |
| `place_peer.go` | Data | Shared tail of admit flows (P20): CreatePeer → HasPeer?Replace:Join (resume presses `ErrPeerAlreadyInRoom` races through via Replace) → Start + rollback → `PeerSession` |
| `sweep_orphan_admitted.go` | Background | grace-expiry: admitted without peer in room and `lastSeen + ReconnectTTL` elapsed → Revoke → `closeRoomIfEmpty` (P31) |
| `sweep_empty_rooms.go` | Background | never-joined (P31): `peers==0` ∧ neither admitted nor unexpired reserve → kill via `closeRoomIfEmpty`; cadence — `RESERVATION_ORPHAN_SWEEP_INTERVAL` |
| `evict_idle_peers.go` | Background | peer with `!IsAlive()` → DisconnectPeer (idle eviction, ADR 0001 + P20); after the batch — `closeRoomIfEmpty` (P31) |

## Create

```text
ValidateNickName (HTTP) → limits → RegisterRoom → CreateRoom
→ IssueTicket(nickName, password) → token + RoomSummary
```

On issue error after a successful create — `DeleteRoom` + `UnregisterRoom`.

## IssueTicket

```text
ValidateNickName → GetRoom → HasPeer? → VerifyRoomPassword
→ AssignHost (HostRegistry, least-loaded) → Reserve(nickName, TTL) [orphan admitted cleanup]
→ Issue(nickName, hostID) → token + RoomSummary + EnetPort
```

Policy A: peer in room → `ErrPeerAlreadyInRoom`; slot held → `ErrTicketSlotHeld`.

**Host sharding (P18):** if `port/transport/enet.HostRegistry` is configured (implementation - `delivery/enet.RoomsHandler`), the use case picks a host for the ticket: least-loaded by live peers, round-robin tie-break. The chosen `HostID` goes into claims (strict binding admit), `Port` - into `IssueTicketResult.EnetPort` → HTTP `enet_port`. Without registry (`nil`) - `AssignHost` is skipped, ticket without binding (`HostIDUnbound`), `EnetPort = 0` (field omitted from the response).

## AdmitPeer (P20)

```text
Validate(token) once → enetHostID strict binding (WS — HostIDUnbound, no check)
→ claims.Kind: join → JoinRoom | reconnect → ReconnectRoom
```

## JoinRoom

```text
GetRoom → Admit → CreatePeer(..., claims.NickName)
→ Replace (if HasPeer) | Join → Start → PeerSession
```

Rollback: on error before a successful join into the room — `Revoke` (except failed Replace: the old peer stays).  
On `Start` failure — `Leave`, `Stop`, `Revoke`.

## ReconnectRoom (P20, deadline P24)

```text
GetRoom → reservation.SlotDetails (read-only lastSeen, no refresh)
→ deadline gate: !HasPeer && lastSeen + reconnectTTL < now → ErrReservationExpired (0x74)
→ reservation.Reconnect (atomic: admitted + refresh lastSeen + slot with NickName)
→ CreatePeer(..., slot.NickName) → HasPeer ? Replace : Join → Start
→ room.Send(OpPeerReconnected{peerID}) to the others → PeerSession
```

`reservation.Admit` is not called; on any failure the slot is not revoked (peer stays in grace). Reject mapping — existing codes (`OpPeerNotFound` / `OpReservationNotReserved` / `OpReservationExpired` / `OpInvalidToken`).

The deadline gate runs **before** `Reconnect` (P24): a rejected attempt does not touch lastSeen and does not extend grace — without the gate, a resume after `lastSeen + RESERVATION_RECONNECT_TTL` would succeed until the sweep passes (with `ORPHAN_SWEEP_INTERVAL=0` — forever). A live peer (`HasPeer`) is unaffected by the gate — the `lastSeen` of a long-connected peer is legitimately old, and newest-wins Replace is available even at `reconnectTTL <= 0` (zero-semantics of sweep: TTL=0 forbids resurrection, not connection takeover). `reconnectTTL` — from `ReservationConfig.ReconnectTTL` (cmd/rooms).

Double-resume race on one token: `Join` returns `ErrPeerAlreadyInRoom` (lifecycle rejects the occupied peerID) → `Replace` presses through — newest-wins (Q10), the evicted object is stopped by `old.Stop()` inside `Replace`.

## DisconnectPeer (P20)

```text
GetPeer(session.PeerID) — stale teardown (peer evicted by Replace / already removed) → no-op before the mark
→ MarkDisconnected (lastSeen = now, slot stays and holds capacity)
→ room.Leave(session.Peer) [ErrPeerStale → no-op; ErrPeerNotFound → warn]
→ peer.Stop → room.Send(OpPeerDisconnected{peerID}) to the others
```

## LeaveRoom

`GetPeer` → `Leave` → `Stop` → `Revoke`. Idempotent: room or peer already removed → `Revoke` + `nil`; reservation slot missing → `nil`. `room.Leave` removes the peer from the room even on policy error — `Stop` and `Revoke` always run, errors are returned via `errors.Join`.

**`ErrRoomShouldClose` (P29):** in `LeaveRoom` and `DisconnectPeer` a sentinel from policy → `closeRoom`: `OpRoomClosed` (`0x82`) to the remaining via `peer.Deliver` → `registry.DeleteRoom` → `reservation.UnregisterRoom` (also revokes the departed peer's slot) → `Stop` all. Grace/reconnect does not apply. Order: Deliver before DeleteRoom (peer lives on `room.Context()`), Stop last (conn close must not overtake the frame; on ENet the order is strict — `DisconnectLater` in the same FIFO).

## Delete

```text
VerifyRoomPassword (Delete only) → GetRoom → closeRoom
(OpRoomClosed 0x82 to all → DeleteRoom → UnregisterRoom → parallel Stop)
```

P31: the serial `LeaveRoom` loop was replaced by the shared `closeRoom` — on a no-migration room the first master-kick triggered policy-death and the remaining `LeaveRoom` + `DeleteRoom` sprayed `ErrRoomNotFound` back (latent bug), plus O(N×drainTimeout). `ErrRoomNotFound` from `DeleteRoom` — tolerated (Warn), race with a parallel death. `DeleteForShutdown` — no password, same path: server shutdown is also "room closed by server" for the client.

## Room death (P31)

Four flows, one teardown:

| Flow | Trigger | Broadcast `0x82` |
|------|---------|------------------|
| Policy-death (`closeRoom`, P29) | `ErrRoomShouldClose` from policy in `LeaveRoom`/`DisconnectPeer` (master left with `allowMasterMigration=false`) | Yes — to remaining |
| Emptied-death (`closeRoomIfEmpty`) | After real peer removal: `LeaveRoom`, `DisconnectPeer`, `EvictIdlePeers` (post-batch), grace-slot revoke in `SweepOrphanAdmitted` | No — no peers |
| Never-joined sweep (`SweepEmptyRoomsUseCase`) | Periodic pass: `peers==0` ∧ neither admitted nor unexpired reserve | No — peers never existed |
| DELETE / shutdown (`Delete`/`DeleteForShutdown`) | Admin / process shutdown | Yes — to all |

"Empty" = `len(room.GetPeers())==0` AND `len(reservation.ListAdmittedPeers)==0`: a grace slot (`MarkDisconnected`, still `admitted`) keeps the room alive until `RESERVATION_RECONNECT_TTL` expires — killing on bare `peers==0` would break P20 resume. Pending reserve tickets do not block transition-kill (the room dies once all joined peers are gone — a late joiner gets a reject, not an empty room); the never-joined sweep, conversely, respects a live reserve — the join may still happen. Kill is idempotent: `ErrRoomNotFound`/`ErrReservationNotFound` on `DeleteRoom`/`UnregisterRoom` — tolerated (Warn). The "join into a dying room" race is the same as for DELETE — accepted.

## EvictIdlePeers

Periodic sweep (`StartIdlePeerSweep`): `registry.GetList` → `room.GetPeers` → `!peer.IsAlive()` → `DisconnectPeer` (P20: a zombie gets the same grace window — `MarkDisconnected` → `Leave` → `Stop` → `OpPeerDisconnected`). An error on one peer is logged, the sweep continues. `interval <= 0` — no-op (nil handle); liveness timeout — inside `WsConnection` (`TransportConfig.PeerIdleTimeout`).

Both sweeps (`StartIdlePeerSweep`, `StartOrphanAdmittedSweep`) return `*SweepHandle`: `Stop()` cancels ctx and waits for the goroutine to finish, including in-flight `RunOnce`.

Sweep use cases accept `port/metrics.Metrics` (nil-safe) and count processed records per pass: `rooms_sweep_processed_total{sweep="idle_peers"}` / `{sweep="orphan_admitted"}` (P19), and each pass — `rooms_sweep_runs_total{sweep=...}` (P19b, separate from processed; `empty_rooms` — third label, P31).

`StartEmptyRoomsSweep` (P31) shares its cadence with the orphan sweep: `RESERVATION_ORPHAN_SWEEP_INTERVAL` (= `ReservationConfig.OrphanSweepInterval`) drives both sweeps, `0` disables both.

## HTTP / transport

Called from `delivery/http`, `delivery/ws`, `delivery/enet` — see their READMEs.
