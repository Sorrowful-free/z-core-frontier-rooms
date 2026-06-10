# domain/state

Authoritative модель комнаты: `RoomState`, maps, patch/apply.

## Типы

- `RoomState` — peers, inputs, entities, metadata
- `PeerState`, `InputState`, `EntityState`, `ComponentState`, `ValueState`
- `MapState`, `MapStatePatch`, `ApplyMapStatePatch`, `ApplyMapStatePatchCopy`

## Apply patch

- Top-level maps (entities и др.) — `ApplyMapStatePatchCopy` на клоне.
- `EntityState.ApplyPatch` — атомарно components + owner после успеха components.
- `ComponentState.ApplyPatch` — атомарно values через `ApplyMapStatePatchCopy`.

## Wire

Бинарный layout payload — [docs/wire-state-codec.md](../../../docs/wire-state-codec.md).  
Codec: `internal/adapter/realtime/codec/`.
