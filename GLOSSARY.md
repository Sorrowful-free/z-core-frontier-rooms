# z-core-frontier-rooms

A game-room service: control plane (HTTP) and data plane (WebSocket, ENet). Domain terms for rooms, peers, and connection liveness.

## Language

**Zombie peer**:
A peer that stays registered in a room while its transport is effectively dead (half-open TCP, etc.). Its reservation slot stays occupied and frames never reach the client.
_Avoid_: dead peer, ghost peer, stale peer

**Idle eviction**:
A periodic sweep that removes zombie peers from rooms through the standard disconnect path (`DisconnectPeer` → `MarkDisconnected` → `room.Leave` → `Peer.Stop`): the peer leaves the room immediately, its reservation slot keeps a grace window for reconnect (P20), and a later sweep revokes it on expiry. Applies to transports whose `IsAlive` reflects idleness (WS); ENet zombies die by the transport's own protocol timeout → `EventDisconnect` → the same `DisconnectPeer` path.
_Avoid_: reaper, cleaner, GC peers

**IsAlive**:
The liveness contract of a connection/peer: `false` means the transport is considered dead and the peer is subject to idle eviction. Each transport implementation decides what counts as alive — for WS it is recent inbound activity, for ENet it is the connection's closed flag (protocol timeouts surface as `EventDisconnect`).
_Avoid_: Connected, Active, Online

**Last seen**:
The internal moment of the client's last confirmed liveness. For WS — a successful inbound frame or pong; outbound frames do not confirm liveness. Not to be confused with RTT (`Ping()`): RTT is a stale-able value, not a timestamp.
_Avoid_: LastActivity, heartbeat timestamp

**ENet host / HostID** (shard):
One ENet host of the process: its own UDP port from `EnetConfig.ListenPorts` and its own `HostLoop`. `domain.HostID` = index+1 into the port list; it lands in ticket claims for strict binding admit (P18). `HostIDUnbound` (`0`) = a ticket without binding (issued without a HostRegistry, WS-only) → `OpWrongHost` reject on ENet admit. A room is transport-agnostic: its peers may sit on different hosts.
_Avoid_: shard index, node, listener id

**Orphan admitted**:
A reservation slot in `admitted` state with no live peer in the room (the client got a ticket and joined, but the peer left/died without a revoke). Cleaned up by the background `SweepOrphanAdmittedUseCase` after `lastSeen + RESERVATION_RECONNECT_TTL` (P20), or by a targeted revoke+reserve inside `IssueTicket` when the same peer re-issues.
_Avoid_: stale slot, dangling slot, orphan reservation

**Grace window** (reconnect window):
The period after a transport break during which the peerID can be revived with a reconnect token. The peer leaves the room immediately (delete-peer model, P20): grace lives on the admitted reservation slot — the slot holds capacity and counts down `disconnectedAt + RESERVATION_RECONNECT_TTL`; on expiry the sweep revokes it.
_Avoid_: reconnect TTL from the issue ticket, suspended peer, offline peer

**Reconnect token**:
An HMAC credential (kind `2`) for resuming under the same peerID: `kind | roomID | peerID | hostID | MAC` — no nickName and no expiry; grace is server-side state on the slot. Distinguished from the join ticket (kind `1`) by the credential's kind byte and by `Claims.Kind` in claims; the one-shot join ticket and the reconnect credential are different entities.
_Avoid_: resume ticket (that term refers to peerID inside a join ticket), session token

**Ghost input**:
An inbound frame (input/RPC/state) from a peer that is no longer in the room (zombie, or removed without state cleanup). The policy rejects such frames with a membership check (`ErrPeerNotFound`), otherwise state would accumulate records from a nonexistent peer.
_Avoid_: phantom input, stale input

**Masterless episode**:
A period when a room has live peers but `p.master == nil` — usually after a failed election. Re-election runs on patch ticks and lazily on RPC→master; the `Error` about the episode is logged once (not per tick), `Info` on recovery.
_Avoid_: election gap, missing master

**Admin listener**:
A separate Fiber instance outside the public API (`ADMIN_LISTEN_ADDR`, empty default = disabled): process-internal endpoints, currently `GET /metrics` (prometheus exposition, P19). Not to be confused with the REST control plane on `HTTP_ADDR` — that one has auth + rate limits, this one is bare telemetry for scraping.
_Avoid_: metrics port, health server, internal API

**Authoritative host** (deploy mode, P33):
The deploy mode `STATE_POLICY_ALLOW_MASTER_MIGRATION=false`, where the master peer is the match's only "host": its departure (leave OR disconnect, grace does not apply) kills the whole room — the rest get `OpRoomClosed` (`0x82`) + disconnect and slots are revoked. Antonym — the default mode with master migration (`true`). Before P33 this was a per-room `policy` field on `POST /rooms`.
_Avoid_: host-pinned, dedicated host, listen server flag

**Host migration** (master migration):
Re-electing the master when it leaves (`electAndAssign`, min ping → min PeerID) — exists only under `STATE_POLICY_ALLOW_MASTER_MIGRATION=true`; in a no-migration deploy there is no migration, the room dies.
_Avoid_: failover host, master handoff

**Master election semantics**:
The rules for choosing a master in `StateRoomPolicy`: stale candidates (`ErrPeerNotFound` from `GetPeer` — peer in state but not in the room) are skipped, elections run among the live; all stale → empty election (`PeerIDInvalid`, not an error); other `GetPeer` errors fail the election; `IsMaster` flags are written into state only after the electee passes validation (validate-first) — state never advertises a master the policy does not have.
_Avoid_: quorum, leader election

**Peer drain** (drain-aware close, P30):
Draining `peer.outbound` at `Peer.Stop` before closing the connection: on ctx cancel the write loop flushes committed frames via `Send`, `Stop` waits for the drain within `TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT` (`0` — disabled) and only then `conn.Close`. The guarantee is "`Send` before `Close`", not "the client received it". Not to be confused with the **grace window**: grace is a reservation-level concept (reviving a peerID after a break), drain is a transport-level flush of final frames on the way out.
_Avoid_: graceful disconnect (that's ENet `DisconnectLater`), send flush, linger

**Tick-coalesced patch** (state emission model, P34):
All `RoomState` deltas accumulate in `p.state` and leave as a single `OpCodePatchState` on the patch tick (`patchStateInterval`, default 50ms) — the only regular emitter; there is no per-message fan-out. Broadcast is universal (the publisher gets an echo of its own deltas, apply is idempotent — soft: `Added`→upsert, `Removed`→ignore). The single immediate exception: full state to the joiner on join.
_Avoid_: per-message replication, delta broadcast, push diff

**Empty-room death** (P31):
Destroying a room that has neither live peers nor admitted reservation slots. Two sub-cases: **emptied** — the last peer left (transition trigger `closeRoomIfEmpty` in `LeaveRoom`/`DisconnectPeer`/`EvictIdlePeers`/orphan-sweep revoke); **never-joined** — the room was created but nobody entered (time-based `SweepEmptyRoomsUseCase`: kill when all reserves have expired or there are no slots). No `0x82` broadcast — nobody to hear it. Distinct from the **authoritative host** deploy (P33): that one kills a room *with* live peers on host departure and broadcasts `0x82` to the rest; shared teardown (`DeleteRoom` → `UnregisterRoom` → Stop), different triggers.
_Avoid_: room GC, idle room cleanup, room reaper
