# Tick-coalesced patch emission: дельты только на patch-тике

## Контекст

Perf-расследование state-пути (bench-замеры, см. план P34) показало:
каждое мутирующее клиентское сообщение (`OpCodeFull/PatchInput`,
`OpCodeFull/PatchEntities`) немедленно порождало broadcast
`OpCodePatchState` — плюс регулярный patch-тик 20 Гц поверх. Цена
per-message пути — 2–3 полных deep clone `RoomState` и полный
O(|state|) diff на КАЖДОЕ сообщение (~1.3 мс / ~1 MB мусора на одно
input-сообщение при 500 entities). При этом tick-patch почти всегда
выходил пустым: дельты уже ушли per-message.

Каденция репликации и так заявлена тиком (`patchStateInterval`,
default 50 мс) — per-message эмиссия дублировала её и разгоняла egress
и аллокации без выигрыша в доставке.

## Decision

- **Единственный регулярный эмиттер `OpCodePatchState` — `OnTickPatchState`.**
  `OnMessage`-обработчики и `OnLeave` мутируют `p.state` точечно
  (copy-on-write записи, без clone/diff/send); join/leave/election/
  input/entities-дельты доезжают клиентам ближайшим тиком
  (задержка ≤ `patchStateInterval`). Immediate-исключение одно:
  `deliverFullStateToPeer` — full state джойнеру сразу на join.
- **Универсальный broadcast.** Тик-патч идёт всем peer без исключений —
  publisher получает эхо своих дельт; client-side apply идемпотентен
  (мягкий apply ниже). `ExcludePeerID` из patch-пути удалён (поле
  `PeerEvent.ExcludePeerID` остаётся — его использует RPC-forward).
- **`prevState` — shallow `RoomState.Snapshot()`**: struct-копия +
  `maps.Clone` трёх верхних мап (~3 аллока) вместо deep clone на каждый
  коммит. Корректно при **COW-инварианте `RoomState`**: записанные
  значения (`InputState.Values`, `EntityState.Components`, `ValueState`)
  не мутируются in-place — мутация только clone-then-replace.
  `commitPrevState` вызывается только после успешного send — сорванная
  рассылка не теряет дельту (она уйдёт следующим тиком).
- **Harden `RoomState.ApplyPatch`**: Inputs-ветка клонирует запись до
  `i1.ApplyPatch` — раньше патчила shared `Values`-мапу in-place
  (единственный нарушитель COW; Entities/Peers-ветки были safe).
- **Мягкий apply (обязательная wire-семантика).** Универсальный
  broadcast + immediate full-state джойнеру порождают штатные кейсы на
  клиентских репликах: `Added` по существующему ключу (эхо, join-dup),
  `Removed` по отсутствующему. `ApplyMapStatePatch`: `Added`→upsert,
  `Removed`→ignore, `Updated` на отсутствующем→ошибка (детектор
  рассинхрона: в корректном потоке ключ обязан быть у всех реплик).
- **Ping едет в full-state.** `refreshPeerPings` сплит: запись
  `Ping()→state` — на `OnTickFullState` до `sendFullState` (full-кадр
  несёт свежие ping, каденция ~4 с); stale-scan + `evTickPrune` —
  `pruneStalePeers` на patch-тике (masterless recovery держит 50 мс,
  а не 4 с). Patch-diff ping не видит: `prevState` коммитится после
  sendFullState с теми же значениями.

## Considered Options

- **Immediate-инкрементальный patch на сообщение** — отклонено: это и
  была старая модель; latency-выигрыш ≤50 мс не оправдывает O(|state|)
  работу и ~5× egress на каждый входящий кадр.
- **Per-peer exclusion (publisher без эха)** — отклонено: универсальный
  broadcast проще (нет exclude-ветки), а эхо безопасно при идемпотентном
  apply; экономия одного peer'а в кадре не стоит отдельного пути.
- **`prevState = deep Clone()`** — отклонено: Snapshot дешевле на два
  порядка; COW-инвариант делает shallow-снапшот корректным прошлым
  состоянием для diff.
- **Ping на patch-тике, как было** — отклонено: diff ping'ов шумел бы
  каждый тик; full-кадр и так несёт всех peers раз в 4 с.
- **Strict apply (Added-dup → ошибка)** — отклонено: ломало бы клиентские
  реплики при универсальном broadcast и immediate join full-state.

## Consequences

- Hot-path `OnMessage`: decode + точечная COW-мутация — O(|patch|)
  вместо O(|state|) clone+diff+encode+send; latency репликации ≤50 мс.
- Удалены `prepareAndSendRoomStatePatch`, `publishRoomStateChange`,
  `sendPatchStateExclude`; `OnLeave` откатывается дешёвым ручным restore
  (shallow `maps.Clone` Peers + запись input + `master.restore`), join —
  encode-only shallow-view без patch-to-others.
- Wire-контракт клиента меняется по семантике apply (мягкий) и каденции
  (дельты только на тике, эхо publisher'у, ping в full-state) — см.
  `docs/client-protocol/state-sync.md`. Форматы кадров не изменились.
- COW-инвариант `RoomState` — теперь поддерживаемый контракт: новые
  мутации state обязаны идти clone-then-replace; `Snapshot` читается
  только как read-view.
