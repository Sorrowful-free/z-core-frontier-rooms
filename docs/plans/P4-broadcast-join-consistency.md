# P4 — Broadcast & join consistency

> **Статус:** done (verified: build/vet/lint clean, go test -race -shuffle=on green).

## Контекст

Ревью нашёл: ad-hoc «peer not found» вместо sentinel, фантомного пира в authoritative state при failed join, мёртвый код и magic-числа. Broadcast-abort при ошибке Deliver **оставляем как есть** (решение) — но README надо привести к факту.

## Находки

1. **Ad-hoc ошибки**: `fmt.Errorf("peer not found: %d")` в room.go:152, room.go:231, room.go:260 и state_room_policy.go:94 — при живом sentinel `domain.ErrPeerNotFound` (уже используется в room.go:177).
2. **Фантомный пир**: `joinPeerAndSyncState` мутирует `p.state.Peers`/`p.master` (server_state_room_policy.go:56-65) **до** send (:67,:76). Send упал → `OnJoin` error → `Room.Join` откатывает только `r.peers` (room.go:137-141) → peer остаётся в `p.state` и разъезжается патчами; `p.master` может указывать на несостоявшегося пира.
3. **Dead code**: `pingUnknown` (master_election.go:11) — не используется.
4. **Magic `-128`** (room.go:68): нижняя граница `int8` в `GetCapacity` — `math.MinInt8` или именованная константа.
5. **README расходится с кодом**: realtime/README формулировки про broadcast («one bad peer mustn't kill broadcast») — фактически `Room.Send` возвращает первую не-`ErrQueueFull` ошибку и обрывает рассылку. Поведение **оставляем**, правим документ.

## Решения (зафиксировано)

- Sentinel: все «peer not found» → `fmt.Errorf("%w: %d", domain.ErrPeerNotFound, peerID)`.
- **Stage-then-commit** для `joinPeerAndSyncState` (выбран reorder, не rollback):
  1. `newState := p.state.Clone()`; мутации (peer в `Peers`, `IsMaster`, выбор `newMaster`) — только на `newState`/локальной переменной.
  2. Send full state джоинеру — payload из `newState` (джоинер видит себя).
  3. Патч `oldRoomState → newState` остальным.
  4. Только после успешных send: `p.state = *newState`, `p.master = newMaster`, `commitPrevState()`.
  - При любой ошибке send — `p.state`/`p.master` не тронуты, rollback не нужен. Джоинер мог получить full state до падения broadcast — безвредно: join отказывается, соединение уйдёт.
- Broadcast abort: код не трогаем; README приводим к факту (не-`ErrQueueFull` ошибка `Deliver` обрывает `Room.Send`, ошибка возвращается вызывающему).
- `-128` → `math.MinInt8`.

## Скоуп правок (override красной зоны — только эти файлы)

- `internal/adapter/realtime/room.go`
- `internal/adapter/realtime/policy/state/server_state_room_policy.go`
- `internal/adapter/realtime/policy/state/state_room_policy.go` (только строка 94 — sentinel)
- `internal/adapter/realtime/policy/state/master_election.go`
- `internal/adapter/realtime/README.md`
- `tests/adapter/realtime/*_test.go`, `tests/adapter/realtime/policy/state/*_test.go` (только под этот план)

## Не трогать (чужие планы)

- `internal/adapter/realtime/peer.go`, `peer_factory.go` — P1
- `internal/adapter/realtime/room_lifecycle.go` — P3
- `internal/adapter/realtime/codec/**`, `policy/state/state_room_policy_factory.go` — P2 (пакет `state` общий с P2 — править только свои файлы)
- `internal/adapter/transport/**`, `internal/delivery/**` — P1/P3

## Шаги

1. `room.go`: три «peer not found» → `ErrPeerNotFound` с `%w`; `-128` → `math.MinInt8` (проверить `capacity` тип — там `int`, сравнение через константу).
2. `state_room_policy.go:94`: тот же sentinel.
3. `server_state_room_policy.go`: перестроить `joinPeerAndSyncState` по stage-then-commit (см. решения); `oldRoomState` уже клонируется — использовать его и `newState`.
4. `master_election.go`: удалить `pingUnknown`.
5. `realtime/README.md`: секция про `Send`/broadcast — описать фактическую семантику обрыва и «peer stopped»; в секцию Shutdown добавить: `peer.outbound` никогда не закрывается — consumer выходит по `ctx.Done()` (конвенция ADR-0003, одна ссылка, не дублировать текст).

## Тесты

- `tests/adapter/realtime/policy/state/`: failed join — send джоинеру/broadcast падает → `p.state.Peers` и `p.master` не изменились, повторный успешный join того же peerID чист (regression на phantom peer). Смотреть существующий harness-тест для policy как образец.
- `tests/adapter/realtime/`: `GetPeer`/targeted send на отсутствующий peer → `errors.Is(err, domain.ErrPeerNotFound)`.
- Существующие `room_join_test.go`, `state_room_policy_test.go` — зелёные.

## Docs

- `internal/adapter/realtime/README.md` — правки по шагу 5 (broadcast abort + ссылка на ADR-0003).
- `docs/client-protocol/state-sync.md` — проверить: если там описано «джоинер получает full state со своей записью» — после stage-commit это сохраняется, правок не нужно (отметить в отчёте).

## Проверки

```bash
go build ./... && go vet ./... && gofmt -l internal/adapter/realtime/
go test -race -shuffle=on -count=1 ./tests/adapter/realtime/... ./tests/...
go test -race -count=5 ./tests/adapter/realtime/policy/state/ ./tests/adapter/realtime/
```
