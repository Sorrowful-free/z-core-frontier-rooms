# domain/state

Authoritative room model: `RoomState`, maps, patch/apply.

## Types

- `RoomState` — peers, inputs, entities, metadata
- `PeerState`, `InputState`, `EntityState` (`EntityTypeID` — entity type), `ComponentState`, `ValueState`
- `MapState`, `MapStatePatch`, `ApplyMapStatePatch`, `ApplyMapStatePatchCopy`

## Copying and the COW invariant

- `RoomState.Clone()` — deep copy for snapshot-before-mutate.
- `RoomState.Snapshot()` — shallow snapshot for reading (diff, encode): struct copy + `maps.Clone` of the three top-level maps; values are shared with the state.
- **COW invariant:** values written into maps (`InputState.Values`, `EntityState.Components`, `ValueState`) are not mutated in-place after being written — mutation is clone-then-replace only. The correctness of `Snapshot` relies on it (P34, prevState in `StateRoomPolicy`).

## Apply patch

- `ApplyMapStatePatch` — **soft** apply (P34): `Added` on an existing key — upsert, `Removed` on a missing key — ignore (a replica may run ahead of the delta stream under tick-coalesced emission: full state to a joiner + echo patches to the sender); `Updated` on a missing key — error (desync detector: the key must exist on all replicas).
- `ApplyMapStatePatchCopy` — same semantics on a clone: the source is not mutated, including on error.
- `RoomState.ApplyPatch` — COW-safe: the Inputs branch clones the record before patching (the stored `Values` map is left untouched).
- `EntityState.ApplyPatch` — atomically applies components + owner + entity type after components succeed.
- `ComponentState.ApplyPatch` — atomically applies values via `ApplyMapStatePatchCopy`.

## Wire

Binary payload layout — [docs/wire-state-codec.md](../../../docs/wire-state-codec.md).  
Codec: `internal/adapter/realtime/codec/`.
