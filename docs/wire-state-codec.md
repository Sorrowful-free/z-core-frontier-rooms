# Wire: state codec (room sync)

Binary contract for game state between the **Go server** and the **Godot client**.  
E2E flow (HTTP → join → sync): [client-protocol/README.md](client-protocol/README.md).  
Implementation: [internal/adapter/realtime/codec/README.md](../internal/adapter/realtime/codec/README.md).  
Error OpCodes: [internal/delivery/errors/README.md](../internal/delivery/errors/README.md).  
Transport frame: `domain.Frame` = `OpCode` (1 byte) + `Payload` (this document describes the **payload** of state/input/rpc).

## General rules

| Rule | Value |
|------|-------|
| Byte order | **Big-endian** for all multi-byte numbers |
| Map (full) | `count u16` + `count` × (`key` + `value`) |
| Map (patch) | `added_count u16` + entries + `updated_count u16` + entries + `removed_count u16` + keys |
| Value payload | `len u8` + `len` raw bytes (opaque, no Go `binary` slice encoding) |
| String | `len u8` + UTF-8 bytes (`nick`) |
| Empty / absent ID | `PeerID(0)`, `RoomID(0)` etc. are invalid; on wire `peer_id u32 = 0` = "no target peer" (RPC) |
| Trailing bytes | Decode rejects a payload when bytes remain after the parsed value (`buf.Len() > 0` → error) |
| Limits | See constants in the codec; encode/decode return an error on overflow |

**IDs in maps:** the map key is the only instance id on the wire. The value body does **not** duplicate `PeerID` / `EntityID` (u16) / `ComponentID` / `ValueId` (except as the map key). The `entity_type_id u8` field inside an entity payload is the entity *type*, not an instance id.

**Input sender:** `RoomEvent.PeerID` on the server; the input payload carries **no** `peer_id`.

## Primitives

```text
u8   — 1 byte
u16  — 2 bytes BE
u32  — 4 bytes BE
i8   — 1 byte (room capacity)
i64  — 8 bytes BE (peer ping)
bool — 1 byte (0 = false, 1 = true; Go `binary.Write` for bool)

string — u8 len + bytes (max len 255)
value  — u8 len + bytes (max len 255)
```

## Value map entry

```text
value_id  u8
value     value (len + bytes)
```

`ValueState` in the domain is an opaque `[]byte`.

## Component (payload in the entity map)

Only a values map (see Value map). Max entries: **32** (`ComponentStateMaxValues`).

```text
component_id u8   — key in entity.Components
payload      — writeComponentState (values map)
```

## Entity (payload in the room map)

```text
entity_type_id u8   — entity type (game enum on the client)
owner          u32
components     map ComponentID → component payload
```

Map key: `EntityID u16` — the instance id. The `entity_type_id` field in the payload is the **type** of the entity and does not duplicate the map key.

Max components: **64** (`EntityStateMaxComponents`).

### Entity patch

```text
flags u8
  bit0 (1) — entity_type_id u8 present
  bit1 (2) — owner u32 present
  bit2 (4) — components map patch present

[entity_type_id u8]
[owner u32]
[components patch]
```

Flags: `EntityStateFlagsEntityTypeID = 1`, `EntityStateFlagsOwner = 2`, `EntityStateFlagsComponents = 4`.

## Peer (payload in the room.Peers map)

```text
nick      string
is_master bool
ping      i64
```

Map key: `peer_id u32`. Max peers in the room map: **255**.

### Peer patch

```text
flags u8
  bit0 (1) — nick string
  bit1 (2) — is_master bool
  bit2 (4) — ping i64

[optional fields in flag order]
```

Flags: `PeerStateFlagsNickName = 1`, `PeerStateFlagsIsMaster = 2`, `PeerStateFlagsPing = 4`.

## Input (opcode `0x05` full / `0x06` patch)

Only a values map. Max values: **32** (`InputStateMaxValues`).

A patch is a map patch (added/updated/removed value entries).

## RPC (opcode `0x07`)

```text
rpc_id    u8
target    u8   — 0 none, 1 peer, 2 master, 3 all
values    map ValueId → value
peer_id   u32  — 0 = no target peer
```

Max values: **32** (`RpcStateMaxValues`).

## Room full (`OpCodeFullState` = `0x01`)

```text
room_id   u32
capacity  i8
peers     map PeerID → peer payload
inputs    map PeerID → input payload
entities  map EntityID → entity payload
```

Limits: peers/inputs **255**, entities **65535**.

The room password is a server secret in `reservation` and is not serialized to the wire.

## Room patch (`OpCodePatchState` = `0x02`)

```text
flags u8
  bit0 (1)  — capacity i8
  bit1 (2)  — peers map patch
  bit2 (4)  — inputs map patch
  bit3 (8)  — entities map patch

[optional sections in bit order]
```

A section is written/read **only if** the corresponding bit is set in `flags`.

Flags: `RoomStateFlagsCapacity = 1`, `RoomStateFlagsPeers = 2`, `RoomStateFlagsInputs = 4`, `RoomStateFlagsEntities = 8`.

## Entities standalone (`OpCodeFullEntities` = `0x03` / `OpCodePatchEntities` = `0x04`)

Standalone opcodes **only from the master** to the server. The payload is **not** a room header, only the entities map:

| OpCode | Hex | Payload |
|--------|-----|---------|
| `OpCodeFullEntities` | `0x03` | `entities` map (full), same as the entities section of room full |
| `OpCodePatchEntities` | `0x04` | `entities` map patch, same as the entities section of room patch |

After receiving them the server merges the entities into the authoritative state; the delta goes out as `OpCodePatchState` (`0x02`) on the next patch tick to **all** peers (tick-coalesced emission, P34 — the master gets the echo). Apply semantics and cadence: [client-protocol/state-sync.md](client-protocol/state-sync.md).

## OpCodes (state traffic)

| OpCode | Hex | Direction | Payload |
|--------|-----|-----------|---------|
| `OpCodeFullState` | `0x01` | server → client | Room full |
| `OpCodePatchState` | `0x02` | server → client | Room patch |
| `OpCodeFullEntities` | `0x03` | master → server | Entities map only |
| `OpCodePatchEntities` | `0x04` | master → server | Entities map patch only |
| `OpCodeFullInput` | `0x05` | client → server | Input map |
| `OpCodePatchInput` | `0x06` | client → server | Input map patch |
| `OpCodeRpc` | `0x07` | client → server → relay | RPC |

Source of truth: [internal/domain/state/opcodes.go](../internal/domain/state/opcodes.go).

Join/reservation/in-room errors: `0x40+` — see [client-protocol/errors.md](client-protocol/errors.md).

## Godot (short)

```gdscript
# u32 BE from PackedByteArray b at offset
func read_u32_be(b: PackedByteArray, off: int) -> int:
    return (b[off] << 24) | (b[off+1] << 16) | (b[off+2] << 8) | b[off+3]

# value after value_id
func read_value(b: PackedByteArray, off: int) -> Dictionary:
    var n = b[off]
    off += 1
    return {"bytes": b.slice(off, off + n), "next": off + n}
```

Keep flag constants and limits in sync with Go (`room_state_codec.go`, `entity_codec.go`, …).

Notes a client should know:

- **Map entry order is unspecified** — the server serializes Go maps, so entries arrive in arbitrary order. Do not rely on ordering.
- **Patch counts are per section** — `added`, `updated`, and `removed` counts are each independently bounded by the section's max, so a patch may legally carry more total entries than the nominal map max. Size buffers accordingly.

## Tests

Round-trip: `go test ./tests/adapter/realtime/codec/...`
