# usecase/room

Сценарии комнаты. Зависит от `port/*`, не от `delivery` и не от конкретных adapter.

## Use case

| Файл | Плоскость | Кратко |
|------|-----------|--------|
| `create.go` | Control | `limits.AllowCreateRoom` → `RegisterRoom` → registry → `IssueTicket` (хост); откат комнаты при ошибке issue |
| `delete.go` | Control | kick peers (`LeaveRoom`) → `DeleteRoom` → `UnregisterRoom` |
| `get_list.go` | Control | Список комнат |
| `issue_ticket.go` | Control | `ValidateNickName` → AssignHost (ENet sharding) → Reserve → Issue → snapshot комнаты (`RoomSummary`) + `EnetPort` |
| `join_room.go` | Data | Validate → Admit → CreatePeer(nick) → Join/Replace → Start |
| `leave_room.go` | Data | Leave → Stop → Revoke |
| `sweep_orphan_admitted.go` | Background | admitted без peer в room → Revoke (шаг 4A) |
| `evict_idle_peers.go` | Background | peer с `!IsAlive()` → LeaveRoom (idle eviction, ADR 0001) |

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

## JoinRoom

```text
Validate → GetRoom → Admit → CreatePeer(..., claims.NickName)
→ Replace (если HasPeer) | Join → Start
```

Откат: при ошибке до успешного join в room — `Revoke` (кроме failed Replace: старый peer остаётся).  
При `Start` failure — `Leave`, `Stop`, `Revoke`.

## LeaveRoom

`GetPeer` → `Leave` → `Stop` → `Revoke`. Идемпотентно: комната или peer уже сняты → `Revoke` + `nil`; слот reservation отсутствует → `nil`. `room.Leave` изымает peer из комнаты даже при ошибке политики — `Stop` и `Revoke` выполняются всегда, ошибки возвращаются через `errors.Join`.

## Delete

```text
VerifyRoomPassword (только Delete) → GetRoom → kick all peers (LeaveRoom)
→ DeleteRoom → UnregisterRoom
```

Ошибки kick логируются; комната всё равно удаляется. `DeleteForShutdown` — без пароля, тот же kick.

## EvictIdlePeers

Периодический sweep (`StartIdlePeerSweep`): `registry.GetList` → `room.GetPeers` → `!peer.IsAlive()` → `LeaveRoom` (`Leave` → `Stop` → `Revoke`, молчаливый `Close`). Ошибка одного peer логируется, sweep продолжается. `interval <= 0` — no-op (nil handle); таймаут живости — внутри `WsConnection` (`TransportConfig.PeerIdleTimeout`).

Оба sweep (`StartIdlePeerSweep`, `StartOrphanAdmittedSweep`) возвращают `*SweepHandle`: `Stop()` отменяет ctx и ждёт завершения горутины, включая in-flight `RunOnce`.

Sweep-use-case'ы принимают `port/metrics.Metrics` (nil-safe) и считают обработанные записи за проход: `rooms_sweep_processed_total{sweep="idle_peers"}` / `{sweep="orphan_admitted"}` (P19), а каждый проход - `rooms_sweep_runs_total{sweep=...}` (P19b, отдельно от processed).

## HTTP / transport

Вызываются из `delivery/http`, `delivery/ws`, `delivery/enet` — см. их README.
