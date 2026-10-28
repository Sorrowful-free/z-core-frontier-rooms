# domain/state

Authoritative модель комнаты: `RoomState`, maps, patch/apply.

## Типы

- `RoomState` — peers, inputs, entities, metadata
- `PeerState`, `InputState`, `EntityState` (`EntityTypeID` — тип сущности), `ComponentState`, `ValueState`
- `MapState`, `MapStatePatch`, `ApplyMapStatePatch`, `ApplyMapStatePatchCopy`

## Копирование и COW-инвариант

- `RoomState.Clone()` — deep copy для snapshot-before-mutate.
- `RoomState.Snapshot()` — shallow-снапшот для чтения (diff, encode): struct-копия + `maps.Clone` трёх верхних мап; значения разделяются со state.
- **COW-инвариант:** записанные в мапы значения (`InputState.Values`, `EntityState.Components`, `ValueState`) после записи не мутируются in-place — мутация только clone-then-replace. На нём держится корректность `Snapshot` (P34, prevState в `StateRoomPolicy`).

## Apply patch

- `ApplyMapStatePatch` — **мягкий** apply (P34): `Added` на существующем ключе — upsert, `Removed` на отсутствующем — ignore (реплика может опережать поток дельт при tick-coalesced эмиссии: full state джойнеру + эхо-патчи отправителю); `Updated` на отсутствующем — ошибка (детектор рассинхрона: ключ обязан быть у всех реплик).
- `ApplyMapStatePatchCopy` — та же семантика на клоне: источник не мутируется и при ошибке.
- `RoomState.ApplyPatch` — COW-safe: Inputs-ветка клонирует запись до патча (хранимая `Values`-мапа не трогается).
- `EntityState.ApplyPatch` — атомарно components + owner + entity type после успеха components.
- `ComponentState.ApplyPatch` — атомарно values через `ApplyMapStatePatchCopy`.

## Wire

Бинарный layout payload — [docs/wire-state-codec.md](../../../docs/wire-state-codec.md).  
Codec: `internal/adapter/realtime/codec/`.
