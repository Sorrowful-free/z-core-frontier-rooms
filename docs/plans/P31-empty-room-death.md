# P31 — смерть пустой комнаты + мгновенный DELETE + фикс active_peers

## Контекст

Всплыло на P27-loadgen smoke (две находки + один латентный баг):

1. **Пустые комнаты не умирают — два класса:**
   - **Emptied**: последний ушедший peer оставляет комнату в registry
     навсегда (`peers:[]`, до DELETE/shutdown) — `drain_complete` в
     loadgen недостижим, registry и reservation копят трупы.
   - **Never-joined**: комната создана (`POST /rooms` + host-тикет), но
     никто не зашёл — reserve-слот чистится только *лениво* при
     следующем `Reserve` на ту же комнату (`sweepExpiredLocked`),
     сама комната висит в registry вечно. Триггер другой —
     time-based, transition-проверки его не накрывают: нужен sweep.
   `DELETE /rooms/:id` — мгновенный disconnect + удаление.
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
2. **Два триггера смерти:**
   - **Transition** — проверка «пустая» на путях реального изъятия
     peer'а (`LeaveRoom`, `DisconnectPeer`, `EvictIdlePeers`, sweep
     ревоки grace-слотов). Kill идемпотентен через `DeleteRoom`
     (`ErrRoomNotFound` — tolerated, как в closeRoom).
   - **Sweep (never-joined, согласовано)** — периодический проход:
     `peers==0` ∧ ни одного `admitted`-слота ∧ все `reserve`-слоты
     истекли (`expiresAt < now`) или слотов нет → kill. Живые
     ticket-claims (unexpired reserve) блокируют kill естественно —
     `createdAt` у комнаты нет и не нужен: истёкший дедлайн самого
     свежего тикета и есть порог. Каденс — тот же
     `RESERVATION_ORPHAN_SWEEP_INTERVAL` (задокументировать покрытие
     обоих sweep'ей), `0` отключает оба.
3. **Унифицировать teardown**: приватный `closeRoomIfEmpty` (то же,
   что `closeRoom`, минус broadcast `0x82` — некому слать):
   `DeleteRoom` → `UnregisterRoom` → (peers уже нет). Именование —
   в словаре `closeRoom`/`OpRoomClosed`, не `kill`.
4. **DELETE = тот же `closeRoom`-путь** (согласовано): убитая
   админом комната шлёт `0x82 OpRoomClosed` выгнанным — семантика
   opcode'а «комната закрыта сервером» совпадает, клиенту не нужен
   второй путь обрыва. Параллельные `peer.Stop` уже есть в closeRoom
   (P30) — «мгновенно» получаем бесплатно. Отклонённая альтернатива:
   отдельный opcode/`0x83 OpRoomDeleted` — клиенту разницы нет,
   комнаты нет в обоих случаях; и молчаливый kick — хуже для
   клиента, чем информированный обрыв. `0x82` действует и на
   `DeleteForShutdown` (согласовано): остановка сервера — тот же
   «комната закрыта сервером» для клиента, параллельные Stop
   быстрее серийного LeaveRoom-цикла.
5. **active_peers фикс — в `Room.Stop()`**: единственная точка,
   через которую умирает любая комната (`registry.DeleteRoom` всегда
   вызывает `room.Stop()`): в `stopOnce` эмитить
   `AddActivePeers(-len(r.peers))` — балансирует ровно те `+1`,
   что не прошли `Leave` (ушедшие через Leave уже получили `-1`,
   в `r.peers` их нет). Никаких usecase-правок для метрики не нужно.
6. **`ErrRoomNotFound` на DELETE — tolerated** (как в `closeRoom`):
   Warn, не ошибка ответа — убирает и баг п.3. Плюс контракт
   «комната удалена»: per-peer ошибки teardown не превращаются в 5xx
   — после `closeRoom`-ошибки повторный `GetRoom` → `ErrRoomNotFound`
   = цель достигнута → Warn + 204.
7. **HTTP leave — `DELETE /rooms/:id/peers/:peerID`** _(superseded P32: self-leave `POST /rooms/:id/leave` по reconnect-токену, kick удалён)_ (решение по
   факту ревью): `LeaveRoomUseCase` был дотянут до провода — живой
   доменный путь (явный уход клиента) на том же control-plane, что
   join. Self-leave = свой peerID; с API-key можно выгнать чужого
   (kick — тот же credential, что у DELETE /rooms, строго слабее).
   Rate limit — тот же `delete`-бакет (тот же класс деструктивных
   операций, новый env не нужен). `ErrPeerNotFound` → 404
   `peer_not_found` (новый код).

## Зона / override

План с явным списком путей = зафиксированный скоуп override по
ai-companion (prod — красная зона).

## Дизайн

### closeRoomIfEmpty (transition-путь)

- Приватный хелпер в `usecase/room` (рядом с `closeRoom`): вход —
  room; если `GetPeers()>0` или `ListAdmittedPeers()>0` — no-op;
  иначе `DeleteRoom` (`ErrRoomNotFound` → Warn, ок) +
  `UnregisterRoom` + `Info "room died empty"`.
- Pending `reserve`-тикеты kill не блокируют (в отличие от sweep-
  предиката): комната умерла по факту ухода всех вошедших —
  опоздавший joiner получит reject (`0x43`/reservation-ошибку), а не
  пустую комнату. Задокументировать в комменте хелпера.
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

### SweepEmptyRooms (never-joined путь)

- Новый `SweepEmptyRoomsUseCase` (`usecase/room/sweep_empty_rooms.go`)
  + `StartEmptyRoomsSweep` по образцу `SweepOrphanAdmitted`/`sweep.go`;
  интервал — `RESERVATION_ORPHAN_SWEEP_INTERVAL` (та же env,
  задокументировать; `0` — оба sweep'я off). Подписка в
  `cmd/rooms/main.go` рядом с существующим sweep.
- `RunOnce`: `registry.GetList()` → для каждой комнаты с
  `GetPeers()==0`: слоты через новый порт-метод (ниже) → kill при
  предикате «ни admitted, ни unexpired reserve». Kill = общий
  `closeRoomIfEmpty`-код без peer-части.
- **Порт расширяется** (изменение против первой редакции «no new
  methods»): `reservation.ListRoomSlots(ctx, roomID)` —
  read-only листинг слотов всех состояний
  (`{PeerID, State, ExpiresAt, LastSeen}` — новый
  `domain.RoomSlotDetail`), зеркало `ListAdmittedPeers`. Mocks:
  `go generate ./tests/mocks/...`.

### DELETE /rooms/:id

- `deleteRoom` → `closeRoom` (общий teardown: `0x82` всем →
  DeleteRoom → UnregisterRoom → параллельные Stop). `kickAllPeers`
  и серийный `LeaveRoom`-цикл уходят; `VerifyRoomPassword` и
  `DeleteForShutdown` — без изменений.
- `ErrRoomNotFound` из `closeRoom`→`DeleteRoom` — Warn (гонка с
  параллельной смертью комнаты), не join в ответ.

### active_peers

- `Room.Stop()` (`room.go`, `stopOnce`): `AddActivePeers(-len(r.peers))`
  — `registry.DeleteRoom` всегда идёт через `room.Stop()`, поэтому
  одна строка покрывает все death-пути (closeRoom, DELETE,
  empty-death, shutdown). Ушедшие через `Leave` уже не в `r.peers`
  и получили свой `-1` — двойного вычета нет.

## Файлы (скоуп)

### Prod (красная, override этим планом)

- `internal/usecase/room/` — хелпер `closeRoomIfEmpty` (отдельный
  файл или в `close_room.go`); вызовы в `leave_room.go`,
  `disconnect_peer.go`, `evict_idle_peers.go`,
  `sweep_orphan_admitted.go`, `delete.go`; новый
  `sweep_empty_rooms.go` (usecase + `StartEmptyRoomsSweep`).
- `internal/adapter/realtime/room.go` — `AddActivePeers(-len(peers))`
  в `Stop`.
- `internal/usecase/room/close_room.go` — reuse для DELETE;
  `ErrRoomNotFound` tolerated уже есть.
- `internal/port/reservation/reservation.go` — `ListRoomSlots`
  (read-only, все состояния).
- `internal/adapter/reservation/reservation.go` — реализация
  `ListRoomSlots`.
- `internal/domain/` — `RoomSlotDetail` (peerID, state, expiresAt,
  lastSeen).
- `cmd/rooms/main.go` — запуск `StartEmptyRoomsSweep` рядом с
  orphan-sweep; возврат конструкции `LeaveRoomUseCase` (живой путь
  через HTTP leave, не dead code).
- `internal/delivery/http/rooms.go` — `LeavePeer` handler + route
  `DELETE /rooms/:id/peers/:peerID` (delete rate limit).
- `internal/delivery/http/parse.go` — `parsePeerIDParam` +
  `peerIDParseAPIError`.
- `internal/delivery/http/errors.go` — `codePeerNotFound` +
  маппинг `ErrPeerNotFound` → 404.

### Tests (зелёная)

- `tests/mocks/` — `go generate` под `ListRoomSlots`.
- `tests/usecase/room/` — closeRoomIfEmpty: последний leave убивает
  комнату; disconnect последнего peer'а с grace — НЕ убивает до TTL;
  sweep-revoke последнего grace-слота — убивает; join в свежую
  комнату — не трогает; DELETE → `0x82` у kicked + unregister +
  tolerated `ErrRoomNotFound`; sweep: never-joined (reserve истёк →
  kill; reserve жив → skip; admitted grace → skip).
- `tests/adapter/realtime/` — gauge: teardown комнаты с N peers →
  `active_peers` возвращается к базе.
- `tests/integration/` — leave-all → `GET /rooms` комнаты нет;
  disconnect + resume последнего peer'а в пределах TTL — работает
  (комната жива); drain_complete в loadgen-сценарии достижим.

### Docs (зелёная)

- `internal/usecase/room/README.md` — потоки смерти комнаты
  (policy-death, emptied-death, never-joined sweep, DELETE).
- `internal/adapter/realtime/README.md` — инвариант gauge на teardown.
- `internal/adapter/reservation/README.md` — `ListRoomSlots`,
  два sweep'я на одном интервале.
- `internal/adapter/config/README.md` — `RESERVATION_ORPHAN_SWEEP_INTERVAL`
  покрывает и empty-room sweep (примечание).
- `internal/domain/README.md` — `RoomSlotDetail`.
- `docs/client-protocol/http.md` — DELETE /rooms: kicked peers
  получают `0x82` перед disconnect; новый роут
  `DELETE /rooms/:id/peers/:peerID` (leave/kick), код
  `peer_not_found`.
- `internal/delivery/http/README.md` — роут + код.
- `docs/client-protocol/errors.md` — у `0x82`: источники — auth-death,
  DELETE, shutdown (не empty-death — слушать некому).
- `GLOSSARY.md` — «empty-room death» (emptied + never-joined) vs
  «authoritative-host death» (разные триггеры, общий teardown).
- `docs/plans/P31-empty-room-death.md` — этот файл.
- ADR не требуется: решение прямое, альтернативы слабые.

### Не трогаем

- Codec, wire-layout — кадры не меняются (`0x82` уже есть).
- Env-контракт — новых переменных нет (sweep переиспользует
  `RESERVATION_ORPHAN_SWEEP_INTERVAL`).

## Открытые детали для реализации

- `RoomSlotDetail` — точная форма (структура vs поля `AdmittedSlot`
  с расширением) — по конвенции `domain`.
- Проверка «пустая» — двумя вызовами (room + reservation) или один
  сводный — по простоте; гонка между ними benign (kill идемпотентен).
- `0x82` на DELETE и shutdown — согласовано (решение 4).

## Verification

1. `go build ./...`
2. `~/go/bin/golangci-lint run ./...`
3. `go test -race -shuffle=on -short -count=1 ./tests/...`
4. `./scripts/enet-verify.sh build` + `vet`
5. Loadgen smoke (inc1): `POLICY_MIX="state:100"` + резкий drain →
   `drain_complete: true` (комнаты умирают, слоты ревокаются) —
   главная приёмка плана.
6. `rooms_active_peers` дельта до/после прогона = 0.
7. Never-joined: `POST /rooms` без join → по истечении
   `ADMISSION_TTL`+sweep-интервала комнаты нет в `GET /rooms`
   (smoke руками или тест с коротким TTL).
