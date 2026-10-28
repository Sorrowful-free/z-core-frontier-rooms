# P34 — tick-coalesced patch emission (state-путь)

## Контекст

Perf-расследование state-пути (отчёт в чате, bench-замеры
`tests/adapter/realtime/policy/state/probe_perf_bench_test.go`):

- Каждое мутирующее клиентское сообщение (`OpCodeFull/PatchInput`,
  `OpCodeFull/PatchEntities`) немедленно порождает broadcast
  `OpCodePatchState` — плюс patch-тик 20Гц поверх. На прогоне
  1000 комнат × ~4.4 peer потенциал ~456–544k кадров/с против ~88k
  тиковых; на проводе 224k/с (остальное — egress-дропы).
- Per-message путь несёт 2–3 полных deep clone `RoomState` + полный
  O(|state|) diff на КАЖДОЕ сообщение: замер 1.3мс/988KB мусора на одно
  input-сообщение при 500 entities (`Clone` ~0.9µs/entity,
  `MakePatch` ~0.6µs/entity).
- p99 patch-тика 18мс — хвост планировки/GC на насыщенных ядрах, не
  собственная стоимость.

Решение: коалесцировать все дельты на patch-тике. Тик и есть заявленная
каденция репликации (`patchStateInterval`); per-message эмиссия
дублировала его и делала tick-patch почти всегда пустым.

## Загрилленные решения (уже согласованы)

1. **All-tick эмиссия.** Единственный регулярный эмиттер patch —
   `OnTickPatchState`. OnMessage-обработчики мутируют `p.state` точечно
   (copy-on-write записи) без clone/diff/send. Исключение-immediate
   одно: `deliverFullStateToPeer` — full state новоприбывшему peer
   сразу на join. Join/leave/election-дельты доезжают тиком (≤50мс).
2. **Универсальный broadcast.** Тик-патч идёт всем без исключений:
   publisher получает эхо своих дельт (client-side apply идемпотентен).
   `ExcludePeerID` из patch-пути уходит (поле PeerEvent остаётся —
   используется RPC-forward).
3. **prevState = shallow-map snapshot.** `prevState` — структура с
   `maps.Clone` трёх верхних мап (~3 аллока), значения разделяются со
   state. Корректно при COW-инварианте: хранимые значения
   (`InputState.Values`, `EntityState.Components`, `ValueState`)
   не мутируются in-place после записи — сегодня так и есть
   (clone-then-replace везде). Инвариант фиксируется комментом на
   `RoomState` + snapshot-isolation property-тестом.
4. **Harden `RoomState.ApplyPatch`.** Единственный потенциальный
   нарушитель COW: Inputs-ветка apply-коллбэка зовёт
   `i1.ApplyPatch` на struct-копии, шарящей `Values`-мапу с хранимой
   записью → клонировать `i1` до патча (Entities/Peers-ветки уже
   COW-safe через `ApplyMapStatePatchCopy`/value-struct).
   ApplyPatch НЕ вызывается на серверном `p.state`/`prevState` ни в
   одном пути — снапшот нужен только для чтения diff'ом.
5. **Ping едет в full-state.** `refreshPeerPings` сплитится:
   запись `Ping()→state` переносится на `OnTickFullState` до
   `sendFullState` (full-кадр несёт свежие ping, каденция 4с,
   нового механизма нет); stale-scan (GetPeer → prune → `evTickPrune`)
   остаётся на patch-тике — привязка к живости/masterless-эпизоду,
   иначе recovery деградирует 50мс→4с. Patch-diff ping не видит:
   после коммита full-state prev и state по ping'ам совпадают.
6. **Идемпотентный apply (wire-semantics).** Универсальный broadcast +
   immediate full-state джойнеру порождают штатные кейсы: джойнер
   получает `Added[себя/ранних peers]` на реплику, где записи уже
   есть; `Removed[peer]` на реплику, где его не было (ушёл до full
   state). `ApplyMapStatePatch` (generic, все уровни):
   - `Added` на существующем ключе → upsert (перезаписать), не ошибка;
   - `Removed` на отсутствующем → ignore;
   - `Updated` на отсутствующем → строгая ошибка (в корректном потоке
     невозможен: ключ обязан быть в prev и new → есть у всех реплик;
     сохраняем как детектор рассинхрона).
   Без этого пункта модель ломает клиентские реплики — обязательная
   часть, не опция. Внешний Godot-клиент — отдельная реализация;
   семантика фиксируется в `docs/client-protocol/state-sync.md`.

## Скоуп правок (зафиксирован)

### Прод-код

- `internal/domain/state/room_state.go`
  - `RoomState.Snapshot()` (имя уточнить при реализации; кандидаты
    `Snapshot`/`ShallowCopy`): struct-копия + `maps.Clone` Peers/Inputs/
    Entities. Коммент: COW-инвариант — значения после записи immutable,
    мутация только clone-then-replace.
  - `ApplyPatch`, Inputs-ветка: `next := i1.Clone();
    next.ApplyPatch(i2); return &next` — COW-safe.
- `internal/domain/state/map_state.go`
  - `ApplyMapStatePatch`: `Added`→upsert, `Removed`→ignore
    (`Updated`-missing остаётся ошибкой). `ApplyMapStatePatchCopy` без
    изменений семантики (через ApplyMapStatePatch получает мягкость).
- `internal/adapter/realtime/policy/state/client_state_room_policy.go`
  - `onClientFullEntities`: master-check → decode →
    `p.state.Entities = full`. Без clone/diff/send.
  - `onClientPatchEntities`: master-check → decode → двухфазный apply:
    staged-результаты по ключам патча (clone только затронутых
    entity → ApplyPatch → собрать), затем запись в `p.state.Entities`
    разом — O(|patch|) и атомарность «ошибка = нет побочек»
    (`ApplyMapStatePatchCopy` clone-all — fallback, если двухфазный
    усложнит код).
  - `onClientFullInput`: membership-check → `p.state.Inputs[pid]=*full`.
  - `onClientPatchInput`: membership-check → clone записи → ApplyPatch
    → `p.state.Inputs[pid]=next` (уже точечный, убрать
    `prepareAndSendRoomStatePatch`-обёртку).
  - `onClientRpc` — без изменений.
- `internal/adapter/realtime/policy/state/server_state_room_policy.go`
  - Сплит `refreshPeerPings`: `pruneStalePeers` (stale-scan +
    delete + `evTickPrune`) — остаётся в `OnTickPatchState`;
    `refreshPeerPings` (только `Ping()→state`) — вызывается в
    `OnTickFullState` до `sendFullState`.
  - `commitPrevState` → `p.prevState = *p.state.Snapshot()`
    (вызывается в `sendFullState`/`sendPatchState` после send —
    единственные точки коммита).
  - `sendPatchStateExclude` — удалить вместе с exclude-аргументом;
    тик-патч — прямой `room.Send` broadcast.
  - `publishRoomStateChange` — удалить (единственный caller —
    entities-хендлеры).
  - `prepareAndSendRoomStatePatch` — удалить; OnLeave переходит на
    дешёвый ручной откат (см. ниже).
  - `joinPeerAndSyncState`: без patch-to-others и без
    commitPrevState. Staging для full-state джойнера — shallow-снапшот
    c записью нового peer (encode-only view), commit — запись peer в
    `p.state.Peers`; `m.peer`-откат через `master.restore` сохраняется.
- `internal/adapter/realtime/policy/state/state_room_policy.go`
  - `OnLeave`: membership-check → `delete(Peers)`,`delete(Inputs)` →
    `master.dispatch(evPeerLeft)`. Дешёвый откат вместо deep-clone
    staging: до мутаций `maps.Clone(p.state.Peers)` (shallow) +
    сохранённая `Inputs[pid]`; при ошибке dispatch — восстановить
    Peers-мапу и запись input + `master.restore`. Sentinel
    `ErrRoomShouldClose` на входе (no-migration) — без изменений.
  - `syncRoomStateFromRoom` → `prevState = *rs.Snapshot()` (две
    независимые мапы-оболочки над пустым state).

### Тесты (переработка + новые)

- `tests/adapter/realtime/policy/state/` — переработать под all-tick:
  - `state_room_policy_test.go`, `invariants_test.go` (viewer-модель
    replay-патчей — ожидание появляется после тика, не сообщения),
    `invariant_property_test.go`, `join_failure_test.go`,
    `leave_atomicity_test.go` (откат — теперь ручной restore, не
    clone-staging), `send_failure_test.go` (send-фейлы возможны только
    на тике/join-full), `malformed_payload_test.go`,
    `master_election_test.go`, `master_recovery_test.go`,
    `refresh_peer_pings_test.go` (сплит: ping — full-тик, prune —
    patch-тик), `no_migration_policy_test.go`.
  - **Новый** snapshot-isolation property-тест: после `Snapshot()`
    любые мутации state (все opcode-пути + prune + assign) не меняют
    снапшот — `Equals`/глубокая сверка.
  - `message_bench_test.go` — починить Added-ловушку: чередовать два
    `Updated`-payload'а (см. probe); бенчи теперь меряют
    decode+точечную мутацию без send.
  - `harness_bench_test.go` — `benchEncodeInputPatch` Added→Updated;
    tick_bench — добавить сценарий с entities в state (нынешний покрывает
    только peers).
- `tests/domain/state/*_test.go` — кейсы мягкого apply
  (Added-existing upsert, Removed-missing ignore, Updated-missing
  error); COW `RoomState.ApplyPatch` (исходник не меняется).
- `tests/adapter/realtime/codec/*_test.go` — round-trip без изменений
  (apply-тесты там зовут ApplyMapStatePatch — проверить, что кейсы не
  опирались на strict-ошибки).
- `tests/integration/match_e2e_client_test.go` — клиентская реплика
  под мягкий apply уже подстроена доменным кодом; проверить
  race-кейс full-state→Added-dup.

### Документация (по факту правок)

- **Новый** `docs/adr/0013-tick-coalesced-patch-emission.md` — решение,
  trade-off (≤patchStateInterval латентность vs ~80% egress +
  O(|state|)→O(1) hot-path), отклонённые альтернативы
  (immediate-инкрементал, per-peer exclusion).
- `docs/client-protocol/state-sync.md` — дельты строго на тике
  (задержка ≤ patchStateInterval), эхо publisher'у, ping-каденция
  full-state, мягкий apply (Added→upsert, Removed→ignore,
  Updated-missing→error).
- `docs/wire-state-codec.md` — ссылка на каденцию/семантику apply,
  если там описаны гарантии apply.
- `GLOSSARY.md` — термин **Tick-coalesced patch** (модель эмиссии:
  дельты копятся в state, единственный эмиттер — patch-тик; исключение
  — full state джойнеру).
- `internal/adapter/realtime/policy/state/README.md` — модель эмиссии,
  сплит prune/ping, COW-инвариант.
- `internal/domain/state/README.md` — `Snapshot()`, COW-инвариант,
  мягкий apply.
- `docs/client-protocol/realtime.md`/`enet.md` — если описывают
  per-message репликацию (сверить по факту).

### Не трогать

- `master_machine.go` — события/диспетчер без изменений (evTickPrune
  живёт в prune-половине сплита).
- `room.go`, `peer.go`, lifecycle, registry, usecase — эмиссия снаружи
  не меняется.
- `PeerEvent.ExcludePeerID` — используется RPC-forward.
- Codec-форматы — wire layout патча не меняется, меняется только
  каденция/apply-семантика.
- Файлы под тегом `enet`.

## Порядок

1. `map_state.go` мягкий apply + `room_state.go` `Snapshot()` + harden
   ApplyPatch + COW-коммент; domain-тесты.
2. Policy: клиентские хендлеры на точечную мутацию; сплит
   prune/ping; commitPrevState→Snapshot; удаление
   `publishRoomStateChange`/`sendPatchStateExclude`/
   `prepareAndSendRoomStatePatch`; join-view; leave-откат.
3. Переработка policy-тестов + snapshot-isolation property + бенчи.
4. Integration/e2e проверка, docs по факту, ADR-0013, GLOSSARY.

## Verification

- `go build ./...` — чисто.
- `go test -race -shuffle=on -short -count=1 ./tests/...` — зелёный;
  CI-эквивалент (без `-short`, e2e) — перед вливанием.
- `~/go/bin/golangci-lint run ./...` — чисто.
- `./scripts/enet-verify.sh build && vet` — только если зацеплен
  tagged-код (ожидается: нет).
- Бенчи до/после: `go test -run xxx -bench
  'BenchmarkOnMessage|BenchmarkOnTickPatchState' -benchmem
  ./tests/adapter/realtime/policy/state/` — ожидание: OnMessage без
  send ~1–3µs на мелком state вместо clone+diff+encode.
- Grep-семейство «per-message patch» (ожидание: 0 вне docs/plans +
  истории): `rg -n 'publishRoomStateChange|sendPatchStateExclude|
  prepareAndSendRoomStatePatch' internal/`
- Нагрузочный контроль: повтор прогона 1000 комнат — ожидание
  `patch_state_recv` ≈ тик-модель (~88k/с × доля dirty-тиков) вместо
  224k/с; `rooms_queue_drops_total{peer_outbound}` — падение.
