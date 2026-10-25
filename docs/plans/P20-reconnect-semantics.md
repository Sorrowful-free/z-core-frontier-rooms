# P20 — Reconnect semantics

## Контекст

Сейчас disconnect = `LeaveRoom`: peer изымается из комнаты, слот reservation
отзывается, peerID умирает навсегда. Вернуться можно только как **новый**
peer: новый тикет, новый peerID — весь entity-ownership, мастерство и
идентичность потеряны. Для сетевой игры с флаки-соединениями это делает
короткий разрыв фатальным для сессии игрока.

## Что уже есть (строительные блоки)

- `Room.Replace(peer)` — leave-then-join под тем же peerID (семантический
  разрыв ~один lifecycle-виток, зафиксировано в P13). Используется для
  reconnect-по-живому-peer'у (Q10).
- `deliverFullStateToPeer` — point-to-point full state на join: механизм
  resync'а уже существует и срабатывает через `OnJoin` при resume.
- Reservation Admit-слот: связка ticket→peerID переживает peer'а —
  admitted-слот и есть носитель grace-окна (см. решения). `peerSlot`
  уже хранит `nickName` (единственный источник nick при resume) и
  `admittedAt` (переиспользуется как lastSeen, T3).
- `SweepOrphanAdmittedUseCase` — уже умеет revoke admitted-слотов без peer
  в комнате; становится expiry-механизмом grace.
- `EvictIdlePeersUseCase` — детектор zombie (!IsAlive); retarget на
  disconnect-путь вместо LeaveRoom.
- Ticket versioning: version byte в admission — reconnect-токен = v4.

## Цель

Короткий разрыв соединения не убивает игровую идентичность: клиент
переподключается и продолжает как тот же peer (свои entities, имя,
peerID). Мастерство не возвращается.

## Решения (зафиксированы гриллингом)

### Модель grace — delete-peer (Q4)

Disconnect **удаляет** peer'а из комнаты сразу: обычный `room.Leave` —
state-запись чистится (`Peers`+`Inputs`; `Entities` с `Owner=peerID`
переживают — захват решает мастер-клиент), election мгновенный,
remove-патч уходит остальным. Peer exists ⟺ connected; мёртвых объектов
в `r.peers` нет — не нужны Send-фильтры, IsAlive-правки election,
флаги статуса, lifecycle-оп mark.

Grace живёт **не на peer'е, а на admitted-слоте reservation** — это
единственная сущность, переживающая peer'а, и она же удерживает capacity:
`Reserve` считает занятые слоты → новые ticket'ы не выдаются, пока слот
в grace. Отличия disconnect-leave от final-leave — две вещи:

1. слот **не ревокается** — `reservation.MarkDisconnected(roomID, peerID)`
   обновляет `admittedAt` на момент разрыва (lastSeen, T3);
2. остальным рассылается `OpPeerDisconnected` (см. Q9).

### Часы grace (Q1, T3, T9)

`deadline = lastSeen + RECONNECT_TTL` — серверное состояние на слоте;
`lastSeen` — переиспользованный `admittedAt` (обновляется на каждый
`MarkDisconnected`/`Reconnect`). Reconnect-токен expiry не несёт (Q8);
credential живёт, пока жив peerID.

`RECONNECT_TTL` — новый env в `ReservationConfig` (TTL слота — домен
reservation); **заменяет** `OrphanAdmittedTTL`: при едином timestamp
их роли сливаются, старый env перестаёт читаться (миграция в
`adapter/config/README.md`).

### Admit-роутинг (T2)

Оба data-plane handler'а (ws `?token=`, enet первый пакет) передают
**сырой token** единому admit use case (`AdmitPeerUseCase` /
единая точка входа): `admission.Validate` один раз → ветвление по
`claims.Kind` → join-поток (`JoinRoom`-логика) или resume-поток
(`ReconnectRoom`-логика, отдельная — другая rollback-семантика).
Сигнатуры внутренних потоков принимают claims — повторного HMAC нет.
Токен выдаётся на HTTP-слое, валидируется только на data-plane.

### Resume-путь (Q2, Q7, Q10, T4, T5)

1. claims `Kind=reconnect` (иначе — join-поток);
2. `GetRoom(claims.RoomID)`;
3. `reservation.Reconnect(roomID, peerID)` — **атомарно** под `room.mu`
   (T5): слот существует и `Admitted` → обновить `admittedAt` (свежий
   lastSeen — sweep его не тронет во время resume) и вернуть слот с
   `NickName`; иначе reject (`OpPeerNotFound`/`OpReservationNotReserved`,
   T10). Гонка sweep-revoke ↔ resume закрыта — отдельный `State()`
   не нужен на этом пути;
4. `CreatePeer(peerID, slot.NickName, conn, ...)` — nick **не из claims**
   (в v4 его нет), а из слота (T4);
5. `room.HasPeer(peerID)` ? `room.Replace(peer)` : `room.Join(peer)` —
   swap по живому peer'у = Replace (Q10: новейшее соединение всегда
   побеждает; старый conn потом попадёт в identity-check teardown'а);
6. `peer.Start()` → broadcast `OpPeerReconnected` остальным (Q5);
7. `reservation.Admit` **не вызывается** — слот уже admitted; при
   **любом** фейле слот не ревокается (peer ещё в grace).

Результат — `RoomSummary` + `PeerSession` (T1).

### Session-handle и идентичность teardown'а (Q3, T1, T8)

`JoinRoom`/`ReconnectRoom` возвращают **`PeerSession{RoomID, PeerID,
Peer}`** — явный контракт сессии; handler несёт его до teardown
(WS: post-waiter; ENet: `sess` хранит после admit).

`runLeaveLocked` удаляет из `r.peers` **только если `r.peers[id] ==
переданный peer` (тот же объект)**. Stale-teardown (старый conn проснулся
после Replace) → **`domain.ErrPeerStale`** (T8): caller мапит в
Debug-лог/no-op — ожидаемая гонка. Чинит и существующую дыру:
`LeaveRoom(peerID)` после Replace сегодня выкинул бы нового peer'а.

### Рассылка opcodes (Q5)

Use case шлёт через `room.Send` — policy/lifecycle не растут:

- `DisconnectPeerUseCase`: `reservation.MarkDisconnected` →
  `room.Leave(peer)` → `peer.Stop()` →
  `room.Send(OpPeerDisconnected{peerID})`. Mark **до** leave: пока peer
  в комнате, sweep игнорирует слот (`HasPeer`) и метка встаёт без
  окна «слот сирота без timestamp». FIFO outbound каждого получателя
  гарантирует порядок «remove-патч → opcode».
- Resume-поток: после успешного Join/Replace →
  `room.Send(OpPeerReconnected{peerID})` (порядок «add-патч → opcode»).

### Триггеры (Q6)

- WS `handleConnect`: `waiter.Wait()` → `DisconnectPeerUseCase`
  вместо `LeaveRoomUseCase`.
- ENet: `EventDisconnect` и admitted-`disconnectSession` (bad frame) →
  `DisconnectPeerUseCase` (sess хранит `PeerSession` после admit).
- `EvictIdlePeersUseCase`: `!IsAlive` → `DisconnectPeerUseCase` — zombie
  получает то же grace-окно (флаки-TCP — целевой сценарий).
- Expiry: `SweepOrphanAdmittedUseCase` — admitted-слот без peer'а и
  `lastSeen + RECONNECT_TTL` прошёл → `Revoke`. Peer уже удалён —
  чистится только слот.

### Reconnect-токен v4 (Q8)

Layout фиксированной длины, 51 байт: `ver(4) | roomID u64 |
peerID u64 | hostID u16 | MAC(32)` — без nickName (nick — из слота),
без timestamps (grace серверный). `domain.Claims` получает `Kind`
(join|reconnect); `admission` — метод `IssueReconnect` + распознавание
v4 в `verify`. ENet strict binding сохраняется: hostID наследует
оригинальное назначение (P18), admit проверяет так же.

`IssueTicket` после `Issue` вызывает `IssueReconnect` →
`IssueTicketResult.ReconnectToken` → HTTP DTO `reconnect_token` в
ответах issue-ticket и create-room.

Cross-transport resume **разрешён** (T6): hostID проверяет только
ENet-admit; peer может воскреситься по другому транспорту — feature,
не баг.

### Opcodes (Q9, T10)

Новый класс **room notifications** в `domain/opcodes.go`, диапазон
`0x80+` (0x60–0x6F — ошибки, state-sync занимает 0x01–0x07):

- `OpPeerDisconnected = 0x80`
- `OpPeerReconnected  = 0x81`

Payload: `peerID u64` BE. Кадры строятся напрямую (`domain.Frame`),
codec не трогаем — state/entities/input/rpc-структуры не меняются.
Reject-маппинг resume (T10): reuse существующих кодов —
`OpPeerNotFound` / `OpReservationNotReserved` / `OpInvalidToken`.

### Метрики (T7)

Минимальный набор по паттерну P19: `PeerDisconnected` (mark),
`ReconnectSucceeded`; expiry едет на существующий
`OrphanAdmittedRevoked`. Reject resume — существующий `JoinRejected`.

## Открытые точки (мелочи при реализации)

- Пакет `PeerSession`: `usecase/room` (возвращается из use case'ов) —
  скорее всего здесь; не `port` — тип оркестрационный.
- `reservation.State()` → расширение до деталей слота (`{Slot, NickName,
  AdmittedAt}`) — нужен ли вообще после появления `Reconnect` (T5):
  остаётся для диагностики/`ListAdmittedPeers`, иначе не расширять.
- Сигнатуры внутренних потоков фасада: `join(ctx, conn, claims)` /
  `reconnect(ctx, conn, claims)` — приватные методы одного use case
  или два use case'а под фасадом — по читаемости кода.

## Скоуп правок (фиксированный)

- `domain`: `Claims.Kind`, `ErrPeerStale`, opcodes 0x80/0x81
  (+комментарий классов), frame-helper для peer-notification при желании.
- `port/admission`: `IssueReconnect`; claims с Kind.
- `adapter/admission`: verify v4, IssueReconnect.
- `port/reservation`: `MarkDisconnected`, `Reconnect`; возможно расширение
  `State()`/`AdmittedSlot` (T4/открытая точка).
- `adapter/reservation`: `MarkDisconnected`/`Reconnect` (refresh
  `admittedAt`), `ReservationConfig.ReconnectTTL`.
- `port/realtime`, `adapter/realtime`: identity-check в `runLeaveLocked`
  (`ErrPeerStale`); `PeerSession` возвращается из use case'ов.
- `usecase/room`: `AdmitPeerUseCase` (фасад validate+роутинг) или
  единый admit-вход; `DisconnectPeerUseCase`; reconnect-логика;
  `EvictIdlePeersUseCase` → disconnect-путь; `SweepOrphanAdmitted` →
  grace-expiry по lastSeen+RECONNECT_TTL; `IssueTicket` → ReconnectToken;
  `JoinRoom`/`ReconnectRoom` → `PeerSession`.
- `adapter/config`: `RECONNECT_TTL` env, удаление `OrphanAdmittedTTL`
  (миграция в README).
- `delivery/ws`, `delivery/enet` (включая `run_cgo.go` → enet-verify!):
  передача token в admit-фасад, teardown → DisconnectPeer с `PeerSession`.
- `delivery/http` DTO + mapper: `reconnect_token`.
- `cmd/rooms`: wire новых use case'ов и `ReservationConfig.ReconnectTTL`.
- Метрики: `PeerDisconnected`, `ReconnectSucceeded` в `port/metrics` +
  `adapter/metrics`.
- Docs: `docs/client-protocol/` (reconnect flow, opcodes 0x80/0x81,
  token v4), README'шки по таблице package-docs, ADR-0008 (есть),
  GLOSSARY (обновлено).

## Верификация

- инвариант-харнес (P10) + property-тесты на disconnect→resume
- e2e: разрыв → reconnect → full state resync → продолжение матча;
  expiry grace → слот освобождён; reconnect-по-живому (Replace) —
  старый teardown не выкидывает нового peer'а (ждёт `ErrPeerStale`)
- `go build ./...`, `go test -race -short -count=1 ./tests/...`;
  при правках `*_cgo.go`/delivery-enet — `./scripts/enet-verify.sh
  build` + `vet` до коммита
