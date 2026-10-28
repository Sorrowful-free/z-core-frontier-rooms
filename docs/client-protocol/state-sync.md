# State sync

Authoritative room state lives on the server (`StateRoomPolicy`). The client **receives** full/patch from the server and **sends** input, entities (master), RPC.

## OpCodes and direction

| OpCode | Hex | Direction | Who may send |
|--------|-----|-----------|--------------|
| FullState | `0x01` | server → all | Server |
| PatchState | `0x02` | server → all | Server |
| FullEntities | `0x03` | client → server | **Master only** |
| PatchEntities | `0x04` | client → server | **Master only** |
| FullInput | `0x05` | client → server | Any peer |
| PatchInput | `0x06` | client → server | Any peer |
| Rpc | `0x07` | client → relay | Any peer |

Payload: [wire-state-codec.md](../wire-state-codec.md). A payload with trailing bytes is an error on the server.

## Server → client

### On join

- The new peer gets `0x01` FullState (a full snapshot, including its own record).
- The rest learn via the join delta on the next `0x02` PatchState (not immediately — see the emission model below). The joiner also receives that patch with `Added[itself]` on top of the full it already has — apply must be idempotent (see "Soft apply").

### Emission model (tick-coalesced, P34)

`0x02` PatchState is the **only regular** delta frame and is sent **only on the patch tick** (default 50 ms): every state change (client→server messages, join/leave, master re-election) accumulates on the server and leaves as a single patch on the tick. Any delta's delay ≤ `patchStateInterval`. The only immediate *state* emission is the full state to the joiner on join (RPC relay and `0x80`/`0x81`/`0x82` notifications are also immediate sends — they are not state frames).

Broadcast is **universal**: the patch goes to every peer without exclusions — the publisher receives an **echo of its own deltas** (e.g. the master sees a patch carrying its own entities changes). Client-side apply is idempotent and must digest this.

| Interval | Frame | Default |
|----------|-------|---------|
| Full state | `0x01` | every **4 s** |
| Patch state | `0x02` | every **50 ms** |

(The intervals are compile-time defaults — `DefaultFullStateInterval`/`DefaultPatchStateInterval`, not env-tunable.)

The client must be able to apply a patch on top of its local state (merge maps per the wire-doc rules).

**Ping:** the `ping` field in peer state is refreshed by the server **only on the full tick** and travels inside `0x01` FullState (~4 s cadence); patch frames carry no ping *updates* (an `Added` peer record inside a patch still carries whatever ping value it had at add time).

### Soft apply (mandatory client semantics)

A replica can run ahead of the delta stream (the joiner's full state already contains its record, the tick patch then sends the same `Added` to everyone; the publisher echo). Therefore map patches are applied softly:

| Section | Key exists on replica | Key missing on replica |
|---------|-----------------------|------------------------|
| `Added` | **upsert** (overwrite) | add |
| `Updated` | apply | **error** — desync, wait for a full state |
| `Removed` | remove | **ignore** |

`Updated` on a missing key is the only strict case: impossible in a correct stream, treat it as a sign of replica desync.

### After client → server

| Client sends | Server does |
|--------------|-------------|
| `0x05` / `0x06` input | Updates `Inputs[peer_id]`; delta to everyone on the next patch tick |
| `0x03` / `0x04` entities (master) | Updates `Entities`; delta to everyone (including the master — echo) on the patch tick |
| `0x07` RPC | Relay by target (below) — the only path with an immediate response to other peers |

## Master

- The **first** peer in a room becomes master.
- **Failover:** among peers with `ping >= 0` — lowest ping; on ties — lowest `peer_id`. If nobody has a known ping (`-1`) — lowest `peer_id`.
- Re-election happens not only when the master leaves: if the last election attempt failed, the server re-elects on the next patch tick or lazily on an incoming `0x07` RPC with `target=master` — `is_master` may flip in any patch.
- `is_master` never points at a dead peer: the server writes the flag only after checking the electee is alive (validate-first), and candidates already absent from the room are skipped by elections.
- In peer state on the wire: the `is_master` field (bool).
- Only the master may send `0x03` / `0x04`. Otherwise the server sends in-room `0x61` `OpInRoomNotMaster` to the sender.

### Godot: master check

```gdscript
func is_local_master(room_state: Dictionary, local_peer_id: int) -> bool:
    var peers = room_state.get("peers", {})
    if not peers.has(local_peer_id):
        return false
    return peers[local_peer_id].get("is_master", false)
```

## Input

- `peer_id` is **not** in the payload — the server takes it from the connection.
- Any peer may send a full (`0x05`) or a patch (`0x06`) of its input map.
- Values are opaque bytes (`ValueId` → blob); the game assigns semantics.

## Entities (master only)

OpCodes `0x03` / `0x04` carry **only** the entities map (not the whole room). After receiving them the server merges them into the authoritative state and sends the room patch to the rest.

Each entity in the map contains `entity_type_id u8` — the entity type for client-side spawn (a game enum). The instance id is the map key `EntityID u16`.

## RPC (`0x07`)

```text
rpc_id  u8
target  u8
values  map
peer_id u32
```

| `target` | Name | Delivery |
|----------|------|----------|
| `0` | none | Error `0x63` invalid RPC |
| `1` | peer | To the single `peer_id` from the payload (must be in the room) |
| `2` | master | To the current master |
| `3` | all | To every peer **except the sender** |

The server is a **relay** — it forwards the same payload (`0x07`) to the recipients; it does not interpret `rpc_id` / values.

## Reconnect (P20)

The reconnect model (P20): a transport break **removes** the peer from the room via the normal leave path — the rest get a remove patch and `0x80` `OpPeerDisconnected`; if the master left, election runs immediately on leave. Identity survives: the admitted reservation slot keeps capacity and `peer_id` within the `RESERVATION_RECONNECT_TTL` grace window (server-side, default `30s`, from the slot's `lastSeen` — the disconnect moment).

- **Resume** — re-admit with the `reconnect_token` (kind `2`) instead of the join ticket (`?token=` / first ENet packet): same `peer_id`, nick from the slot (the token carries none). Success — like a regular join: the returner gets `0x01` FullState, the rest get `0x81` `OpPeerReconnected`. Flow and reject codes: [realtime.md](realtime.md#reconnect-p20), [errors.md](errors.md).
- **Mastery is not restored:** the election at disconnect already handed `is_master` to someone else — the returner joins as a regular peer and becomes master only under the general rules above (first in an emptied room / failover).
- **Replace (newest-wins):** resuming while the old conn is **alive** evicts it — the old connection is torn down; the grace deadline does not apply to this path.
- Errors: `0x45` `OpReplaceFailed` / `0x51` `OpPeerStartFailed` — same as join; expired grace → `0x74` `OpReservationExpired` (if the sweep already revoked the slot → `0x73`); after that only a new issue ticket (new `peer_id`).

## Recommended Godot structure

```text
RoomClient          — HTTP + WS/ENet lifecycle
WireCodec           — encode/decode payloads (wire-state-codec)
RoomStateModel      — local copy of the peers/inputs/entities maps
StateSync           — apply full/patch, diff for outgoing input
MasterLogic         — entities update only while is_master
RpcHandler          — encode/decode 0x07 by game RpcIDs
```

Source of server behavior: `internal/adapter/realtime/policy/state/`.
