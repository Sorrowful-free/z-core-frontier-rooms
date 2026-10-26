# usecase/room

Сценарии комнаты. Зависит от `port/*`, не от `delivery` и не от конкретных adapter.

## Use case

| Файл | Плоскость | Кратко |
|------|-----------|--------|
| `create.go` | Control | `limits.AllowCreateRoom` → `RegisterRoom` → registry → `IssueTicket` (хост); откат комнаты при ошибке issue |
| `delete.go` | Control | kick peers (`LeaveRoom`) → `DeleteRoom` → `UnregisterRoom` |
| `get_list.go` | Control | Список комнат |
| `issue_ticket.go` | Control | `ValidateNickName` → AssignHost (ENet sharding) → Reserve → Issue + IssueReconnect → snapshot комнаты (`RoomSummary`) + `EnetPort` + reconnect-токен |
| `admit_peer.go` | Data | Фасад (P20): Validate один раз → claims.Kind → join или resume; ENet strict binding (enetHostID) |
| `join_room.go` | Data | Admit → CreatePeer(nick) → Join/Replace → Start → `PeerSession` |
| `reconnect_room.go` | Data | Resume (P20/P24): `reservation.SlotDetails` → deadline-гейт (`ErrReservationExpired`) → `reservation.Reconnect` → CreatePeer(nick из слота) → Join/Replace → Start → `OpPeerReconnected` |
| `disconnect_peer.go` | Data | Разрыв в grace (P20): `GetPeer` identity-check → `MarkDisconnected` → Leave → Stop → `OpPeerDisconnected` |
| `leave_room.go` | Data | Leave → Stop → Revoke (явный leave / rollback) |
| `peer_session.go` | Data | `PeerSession{RoomID, PeerID, Peer}` — session-handle до teardown (P20) |
| `place_peer.go` | Data | Общий хвост admit-потоков (P20): CreatePeer → HasPeer?Replace:Join (resume дожимает `ErrPeerAlreadyInRoom` гонки через Replace) → Start + rollback → `PeerSession` |
| `sweep_orphan_admitted.go` | Background | grace-expiry: admitted без peer в room и `lastSeen + ReconnectTTL` прошёл → Revoke |
| `evict_idle_peers.go` | Background | peer с `!IsAlive()` → DisconnectPeer (idle eviction, ADR 0001 + P20) |

## Create

```text
ValidateNickName (HTTP) → limits → RegisterRoom → CreateRoom
→ IssueTicket(nickName, password) → token + RoomSummary
```

При ошибке issue после успешного create — `DeleteRoom` + `UnregisterRoom`.

## IssueTicket

```text
ValidateNickName → GetRoom → HasPeer? → VerifyRoomPassword
→ AssignHost (HostRegistry, least-loaded) → Reserve(nickName, TTL) [orphan admitted cleanup]
→ Issue(nickName, hostID) → token + RoomSummary + EnetPort
```

Политика A: peer в room → `ErrPeerAlreadyInRoom`; слот held → `ErrTicketSlotHeld`.

**Host sharding (P18):** если сконфигурирован `port/transport/enet.HostRegistry` (реализация - `delivery/enet.RoomsHandler`), use case выбирает host для ticket'а: least-loaded по живым peer'ам, round-robin tie-break. Выбранный `HostID` идёт в claims (strict binding admit), `Port` - в `IssueTicketResult.EnetPort` → HTTP `enet_port`. Без registry (`nil`) - `AssignHost` пропускается, ticket без привязки (`HostIDAny`), `EnetPort = 0` (поле в ответе опускается).

## AdmitPeer (P20)

```text
Validate(token) один раз → enetHostID strict binding (WS — HostIDAny, без проверки)
→ claims.Kind: join → JoinRoom | reconnect → ReconnectRoom
```

## JoinRoom

```text
GetRoom → Admit → CreatePeer(..., claims.NickName)
→ Replace (если HasPeer) | Join → Start → PeerSession
```

Откат: при ошибке до успешного join в room — `Revoke` (кроме failed Replace: старый peer остаётся).  
При `Start` failure — `Leave`, `Stop`, `Revoke`.

## ReconnectRoom (P20, дедлайн P24)

```text
GetRoom → reservation.SlotDetails (read-only lastSeen, без refresh'а)
→ deadline-гейт: !HasPeer && lastSeen + reconnectTTL < now → ErrReservationExpired (0x74)
→ reservation.Reconnect (атомарно: admitted + refresh lastSeen + слот с NickName)
→ CreatePeer(..., slot.NickName) → HasPeer ? Replace : Join → Start
→ room.Send(OpPeerReconnected{peerID}) остальным → PeerSession
```

`reservation.Admit` не вызывается; при любом фейле слот не ревокается (peer в grace). Reject-маппинг — существующие коды (`OpPeerNotFound` / `OpReservationNotReserved` / `OpReservationExpired` / `OpInvalidToken`).

Дедлайн-гейт идёт **до** `Reconnect` (P24): rejected попытка не трогает lastSeen и не продлевает grace — без гейта resume после `lastSeen + RESERVATION_RECONNECT_TTL` был бы успешен до прохода sweep (при `ORPHAN_SWEEP_INTERVAL=0` — бессмертно). Живой peer (`HasPeer`) гейтом не затрагивается — `lastSeen` давно подключённого peer'а старый законно, newest-wins Replace доступен и при `reconnectTTL <= 0` (ноль-семантизм sweep: TTL=0 запрещает воскрешение, не перехват соединения). `reconnectTTL` — из `ReservationConfig.ReconnectTTL` (cmd/rooms).

Гонка double-resume одного токена: `Join` возвращает `ErrPeerAlreadyInRoom` (lifecycle отклоняет занятый peerID) → дожим `Replace` — newest-wins (Q10), вытесненный объект останавливается `old.Stop()` внутри `Replace`.

## DisconnectPeer (P20)

```text
GetPeer(session.PeerID) — stale teardown (peer вытеснен Replace / уже удалён) → no-op до mark'а
→ MarkDisconnected (lastSeen = now, слот остаётся и держит capacity)
→ room.Leave(session.Peer) [ErrPeerStale → no-op; ErrPeerNotFound → warn]
→ peer.Stop → room.Send(OpPeerDisconnected{peerID}) остальным
```

## LeaveRoom

`GetPeer` → `Leave` → `Stop` → `Revoke`. Идемпотентно: комната или peer уже сняты → `Revoke` + `nil`; слот reservation отсутствует → `nil`. `room.Leave` изымает peer из комнаты даже при ошибке политики — `Stop` и `Revoke` выполняются всегда, ошибки возвращаются через `errors.Join`.

## Delete

```text
VerifyRoomPassword (только Delete) → GetRoom → kick all peers (LeaveRoom)
→ DeleteRoom → UnregisterRoom
```

Ошибки kick логируются; комната всё равно удаляется. `DeleteForShutdown` — без пароля, тот же kick.

## EvictIdlePeers

Периодический sweep (`StartIdlePeerSweep`): `registry.GetList` → `room.GetPeers` → `!peer.IsAlive()` → `DisconnectPeer` (P20: zombie получает то же grace-окно — `MarkDisconnected` → `Leave` → `Stop` → `OpPeerDisconnected`). Ошибка одного peer логируется, sweep продолжается. `interval <= 0` — no-op (nil handle); таймаут живости — внутри `WsConnection` (`TransportConfig.PeerIdleTimeout`).

Оба sweep (`StartIdlePeerSweep`, `StartOrphanAdmittedSweep`) возвращают `*SweepHandle`: `Stop()` отменяет ctx и ждёт завершения горутины, включая in-flight `RunOnce`.

Sweep-use-case'ы принимают `port/metrics.Metrics` (nil-safe) и считают обработанные записи за проход: `rooms_sweep_processed_total{sweep="idle_peers"}` / `{sweep="orphan_admitted"}` (P19), а каждый проход - `rooms_sweep_runs_total{sweep=...}` (P19b, отдельно от processed).

## HTTP / transport

Вызываются из `delivery/http`, `delivery/ws`, `delivery/enet` — см. их README.
