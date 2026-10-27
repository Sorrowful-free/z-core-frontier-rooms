# P31 — смерть пустой комнаты + мгновенный DELETE + фикс active_peers

## Контекст

Всплыло на P27-loadgen smoke (две находки + один латентный баг):

1. **Пустые комнаты не умирают.** Последний ушедший peer оставляет
   комнату в registry навсегда (`peers:[]`, до DELETE/shutdown) —
   `drain_complete` в loadgen недостижим для `state`-комнат, registry
   и reservation копят трупы комнат. Решение пользователя: комната,
   опустевшая, **умирает сама**; `DELETE /rooms/:id` — мгновенный
   disconnect + удаление.
2. **`rooms_active_peers` дрейфит на teardown-путях**: gauge +1/-1
   эмитится в `room_lifecycle.go` на join/leave, но `closeRoom`
   (P29/P30) и `kickAllPeers` убивают peers мимо `Leave` — `-N` не
   эмитится, gauge завышен на число убитых (P19b-инвариант
   «gauge по факту мутации peers map» нарушен на смерти комнаты).
3. **Латентный баг (post-P29):** `DELETE` на `authoritative-host`
   комнате: `kickAllPeers` идёт серийным `LeaveRoom` → первый же
   master-kick триггерит `closeRoom` → остальные `LeaveRoom` и сам
   `registry.DeleteRoom` сыплют `ErrRoomNotFound` → ошибка
   уезжает в HTTP-ответ. Плюс серийный `LeaveRoom` с drain-aware
   `Stop` (P30) — O(N×drainTimeout) на каждый DELETE.

## Решения (загрилленные — согласованы)

1. **«Пустая» = `len(room.GetPeers())==0` И
   `len(reservation.ListAdmittedPeers(roomID))==0`** — ни живых peers,
   ни grace-слотов. Grace-слот остаётся `admitted` (MarkDisconnected)
   и держит reconnect для ушедшего: комната с grace живёт до
   истечения `RESERVATION_RECONNECT_TTL`. **Не** убивать по голому
   `peers==0` — иначе одиночный disconnect последнего peer'а ломает
   P20-resume (комната умрёт мгновенно, вернуться некуда).
2. **Триггер — transition-based**: проверка «пустая» только на путях
   реального изъятия peer'а (`LeaveRoom`, `DisconnectPeer`,
   `EvictIdlePeers`, sweep ревоки grace-слотов) — комната «создана,
   host ещё не зашёл» под проверку не попадает никогда (никого не
   изымали). Kill идемпотентен через `DeleteRoom` (`ErrRoomNotFound`
   — tolerated, как в closeRoom).
3. **Унифицировать teardown**: приватный `killRoom` (то же, что
   `closeRoom`, минус broadcast `0x82` — некому слать):
   `DeleteRoom` → `UnregisterRoom` → (peers уже нет).
4. **DELETE = тот же `closeRoom`-путь** (согласовано): убитая
   админом комната шлёт `0x82 OpRoomClosed` выгнанным — семантика
   opcode'а «комната закрыта сервером» совпадает, клиенту не нужен
   второй путь обрыва. Параллельные `peer.Stop` уже есть в closeRoom
   (P30) — «мгновенно» получаем бесплатно. Отклонённая альтернатива:
   отдельный opcode/`0x83 OpRoomDeleted` — клиенту разницы нет,
   комнаты нет в обоих случаях; и молчаливый kick — хуже для
   клиента, чем информированный обрыв.
5. **active_peers фикс**: при смерти комнаты эмитить
   `AddActivePeers(-len(r.peers))` — единая точка в room teardown
   (не в каждом usecase-пути): `Room.Stop()`/`runLifecycle`-exit —
   точку уточнить на реализации, инвариант «gauge по факту ухода
   peers из комнаты любым путём».
6. **`ErrRoomNotFound` на DELETE — tolerated** (как в `closeRoom`):
   Warn, не ошибка ответа — убирает и баг п.3.

## Зона / override

План с явным списком путей = зафиксированный скоуп override по
ai-companion (prod — красная зона).

## Дизайн

### killRoomIfEmpty

- Приватный хелпер в `usecase/room` (рядом с `closeRoom`): вход —
  room; если `GetPeers()>0` или `ListAdmittedPeers()>0` — no-op;
  иначе `DeleteRoom` (`ErrRoomNotFound` → Warn, ок) +
  `UnregisterRoom` + `Info "room died empty"`.
- Вызовы: после успешного изъятия peer'а в `LeaveRoom`,
  `DisconnectPeer` (оба — после `peer.Stop`, до return), в
  `EvictIdlePeers` после батча evict'ов, в
  `SweepOrphanAdmitted.RunOnce` — для комнаты, у которой отозван
  grace-слот (именно там «пустая» наступает по истечении TTL
  последнего reconnect-окна).
- Гонка «join попал в умирающую комнату»: join между проверкой и
  `DeleteRoom` — peer приземляется в мёртвую комнату, его ctx умирает
  с `room.Stop()`; та же семантика гонки, что у DELETE сегодня —
  принимаем без дополнительных гарантий.

### DELETE /rooms/:id

- `deleteRoom` → `closeRoom` (общий teardown: `0x82` всем →
  DeleteRoom → UnregisterRoom → параллельные Stop). `kickAllPeers`
  и серийный `LeaveRoom`-цикл уходят; `VerifyRoomPassword` и
  `DeleteForShutdown` — без изменений.
- `ErrRoomNotFound` из `closeRoom`→`DeleteRoom` — Warn (гонка с
  параллельной смертью комнаты), не join в ответ.

### active_peers

- Точка эмита — в адаптере realtime на фактическом teardown комнаты
  (где peers массово выбывают без per-peer `Leave`): вычесть
  `len(peers)` разом. Уточнить место: `Room.Stop()` — единственная
  точка, через которую умирает любая комната; либо
  `closeRoom`-уровень. Не дублировать per-peer `-1` с уже
  отработавшими `Leave`-hooks (учесть в реализации: вычитать только
  оставшихся `len(peers)`, а не всю историю).

## Файлы (скоуп)

### Prod (красная, override этим планом)

- `internal/usecase/room/` — новый хелпер kill-if-empty (отдельный
  файл или в `close_room.go`); вызовы в `leave_room.go`,
  `disconnect_peer.go`, `evict_idle_peers.go`,
  `sweep_orphan_admitted.go`, `delete.go`.
- `internal/adapter/realtime/` — `room_lifecycle.go`/где живёт room
  teardown: `AddActivePeers(-len(peers))` при смерти комнаты.
- `internal/usecase/room/close_room.go` — `ErrRoomNotFound` tolerated
  уже есть; при reuse для DELETE — без изменений либо выделить общий
  кусок.

### Tests (зелёная)

- `tests/usecase/room/` — kill-if-empty: последний leave убивает
  комнату; disconnect последнего peer'а с grace — НЕ убивает до TTL;
  sweep-revoke последнего grace-слота — убивает; join в свежую
  комнату — не трогает; DELETE → `0x82` у kicked + unregister +
  tolerated `ErrRoomNotFound`.
- `tests/adapter/realtime/` — gauge: teardown комнаты с N peers →
  `active_peers` возвращается к базе.
- `tests/integration/` — leave-all → `GET /rooms` комнаты нет;
  disconnect + resume последнего peer'а в пределах TTL — работает
  (комната жива); drain_complete в loadgen-сценарии достижим.

### Docs (зелёная)

- `internal/usecase/room/README.md` — потоки смерти комнаты
  (policy-death, empty-death, DELETE).
- `internal/adapter/realtime/README.md` — инвариант gauge на teardown.
- `docs/client-protocol/http.md` — DELETE: kicked peers получают
  `0x82` перед disconnect.
- `docs/client-protocol/errors.md` — у `0x82`: источник теперь и
  DELETE, не только authoritative-host.
- `GLOSSARY.md` — «empty-room death» vs «authoritative-host death»
  (разные триггеры, общий teardown).
- `docs/plans/P31-empty-room-death.md` — этот файл.
- ADR не требуется: решение прямое, альтернативы слабые.

### Не трогаем

- Port-сигнатуры: `ListAdmittedPeers` уже есть — новых методов не
  нужно.
- Никнейм/pending-reservation («host ещё не зашёл») комнаты —
  существующее поведение (ticket TTL/orphan sweep), вне скоупа.

## Открытые детали для реализации

- Точная точка `AddActivePeers(-n)` (Room.Stop vs usecase) — по факту
  кода lifecycle; принцип — один эмит на фактическое выбытие peers.
- Проверка «пустая» — двумя вызовами (room + reservation) или один
  сводный — по простоте; гонка между ними benign (kill идемпотентен).
- `0x82` на DELETE — согласовано (решение 4).

## Verification

1. `go build ./...`
2. `~/go/bin/golangci-lint run ./...`
3. `go test -race -shuffle=on -short -count=1 ./tests/...`
4. `./scripts/enet-verify.sh build` + `vet`
5. Loadgen smoke (inc1): `POLICY_MIX="state:100"` + резкий drain →
   `drain_complete: true` (комнаты умирают, слоты ревокаются) —
   главная приёмка плана.
6. `rooms_active_peers` дельта до/после прогона = 0.
