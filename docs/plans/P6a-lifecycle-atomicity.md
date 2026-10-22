# P6a — Lifecycle atomicity

> **Статус:** done (verified: build/vet/lint clean, go test -race -shuffle=on green).

## Контекст

Critical-баг аудита: `Room.Leave` удаляет peer из `r.peers` только после успеха `OnLeave`; ошибка политики → peer навсегда в реестре (повторный `Leave` падает `ErrPeerNotFound`, sweep бессилен). Компаунд: `prepareAndSendRoomStatePatch` мутирует `p.state` на месте — при ошибке `electAndAssignMaster` state полумутирован, `p.master` на ушедшем peer → все entity-операции `ErrNotMaster`.

## Решения (D4)

- `Leave` удаляет из `r.peers` **всегда**; ошибка политики — лог + возврат вызывающему.
- `prepareAndSendRoomStatePatch` — stage-then-commit (как `joinPeerAndSyncState` в P4): `applyChanges` на клоне, commit после успеха.
- `Join`: insert в `r.peers` **после** успешного `dispatchLifecycle` — закрывает окно «в мапе, но не в политике».

## Скоуп правок (override красной зоны)

- `internal/adapter/realtime/room.go`
- `internal/adapter/realtime/policy/state/server_state_room_policy.go`
- `internal/adapter/realtime/policy/state/client_state_room_policy.go`
- `internal/usecase/room/leave_room.go`
- `internal/usecase/room/join_room.go` — **только** логирование rollback-ошибок (строки 79-80); admit-маппинг — P6b
- `tests/adapter/realtime/**`, `tests/usecase/room/{leave_room,join_room}_test.go`

## Не трогать

- `peer.go`, `room_lifecycle.go`, `state_room_policy.go` — вне бага.
- `codec/**` — P6d.

## Шаги

1. `server_state_room_policy.go` `prepareAndSendRoomStatePatch`: клон `p.state` → `applyChanges(clone)` → `MakePatch(old,clone)` → send → `p.state = *clone`. Откат не нужен — на ошибке `p.state` не тронут.
2. `room.go` `Leave`: проверка членства под `RLock` → `dispatchLifecycle` → **удаление из мапы независимо от результата** (ошибку политики логировать через `r.logger` и возвращать).
3. `room.go` `Join`: `RLock` capacity-check → `dispatchLifecycle` → `Lock` + re-check capacity + insert. При ошибке dispatch — ничего откатывать не надо (мапа не тронута).
4. `room.go` `Start`: `sync.Once`-гвард; тикеры создавать до `go processRoomEvents` (уже так — добавить только guard и зафиксировать порядок чтения в Stop).
5. `client_state_room_policy.go` `onClientFullInput`/`onClientPatchInput`: `_, ok := roomState.Peers[peerID]; if !ok → ErrPeerNotFound` до записи `Inputs` — ghost input от zombie/replaced peer.
6. `leave_room.go:61-64`: `Revoke` выполняется даже при ошибке `peer.Stop()` — `errors.Join(stopErr, revokeErr)`.
7. `join_room.go:79-80`: `_ = room.Leave(peer); _ = peer.Stop()` → залогировать ошибки rollback (не глотать).

## Тесты

- Regression вечного zombie: policy `OnLeave` падает (стаб `failOnLeavePolicy` или мок после P6e-mockgen) → `Leave` вернул ошибку, но `room.HasPeer(id)` = false; повторный `Leave` → `ErrPeerNotFound`, не залипает.
- `state.Peers` не мутируется при упавшем `OnLeave` (election-ошибка): state до и после идентичен.
- Ghost input: `Deliver` от peerID не в `Peers` → `ErrPeerNotFound`, `Inputs` пуст.
- `Join` двойной/гонку — вставка только после политики: failed `OnJoin` → `HasPeer` = false.
- `leave_room`: `peer.Stop` ошибка → `Revoke` всё равно вызван (assert через mock reservation).
- Существующие `room_join_test.go`, `room_stop_test.go`, `state_room_policy_test.go` — зелёные.

## Проверки

```bash
go build ./... && go test -race -count=3 ./tests/adapter/realtime/... ./tests/usecase/room/...
```
