# P10 — executable invariants for StateRoomPolicy

Зелёная зона: только `tests/adapter/realtime/policy/state/`. Production не трогаем.
Цель: инварианты, которые ломали последние волны, становятся исполняемыми —
нарушение = красный тест сразу, а не находка на ревью.

## Дизайн (зафиксирован гриллингом)

### Наблюдение — shadow-state

`p.master`/`p.state` неэкспортируемы. Проверяем клиент-видимое состояние:
capturePeer в комнате получает все broadcast; харнес материализует
`state.RoomState` из full-state и patch кадров (`RoomStateCodec.Decode` +
`DecodePatch`/`ApplyPatch`) — ровно то, что видит клиент, без сайд-эффектов.

### Два уровня инвариантов

**Слабые — post-op (после каждой операции, без settle):**
1. `IsMaster` выставлен не более чем у одного peer
2. `IsMaster` peer присутствует в записях `Peers` wire-state (флаг у живой
   записи, не у фантомной)

**Сильные — settle (после одного patch-тика, когда prune-окно закрылось):**
3. `IsMaster=true` ⇒ `room.HasPeer(id)` — флаг не рекламирует мёртвого peer
   (validate-first; до тика легальный lag: мастер умер нечисто, запись ждёт
   prune — это не нарушение)
4. `IsMaster` ≤ 1 сохраняется и после prune/re-elect

### Property-прогон

`invariant_property_test.go` — seeded `math/rand`, 5 фиксированных seed'ов,
~300 шагов каждый; на падении лог seed + трасса операций. Словарь шагов:
- clean: `Join`, `Leave`, `OnTickPatchState`, `OnTickFullState`, `OnMessage`
  (input/rpc от случайного peer)
- fault-инъекции (иначе stale/masterless недостижимы): `setErr` на Deliver
  перед Leave (stale-запись в state), `IsAlive=false` (смерть без leave —
  prune/election путь), `peer.Stop`

После каждого clean/fault шага — слабые инварианты; периодически (каждые
~25 шагов и в конце) — settle-тик + сильные.

### Обвязка существующих тестов

Однострочные вызовы после операций в `master_recovery_test.go`,
`master_election_test.go`, `leave_atomicity_test.go`, `send_failure_test.go`.

## Файлы

- `invariants_test.go` — shadow-state хелпер (`shadowRoom` обёртка над
  capturePeer'ами + материализация), `assertWeakInvariants`,
  `assertStrongInvariants`
- `invariant_property_test.go` — property-прогон
- правки в перечисленных существующих тестах — только добавление вызовов

## Критерий выхода

- `go test -race -shuffle=on -count=1 ./tests/adapter/realtime/policy/state/` зелёный
- sanity: ручное нарушение инварианта (тест с заведомо битым capturePeer,
  рекламирующим IsMaster после смерти) ловится ассертом
