# usecase/room

Сценарии комнаты. Зависит от `port/*`, не от `delivery` и не от конкретных adapter.

## Use case

| Файл | Плоскость | Кратко |
|------|-----------|--------|
| `create.go` | Control | `limits.AllowCreateRoom` → `RegisterRoom` → registry → `IssueTicket` (хост); откат комнаты при ошибке issue |
| `delete.go` | Control | kick peers (`LeaveRoom`) → `DeleteRoom` → `UnregisterRoom` |
| `get_list.go` | Control | Список комнат |
| `issue_ticket.go` | Control | `ValidateNickName` → Reserve → Issue → snapshot комнаты (`RoomSummary`) |
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
→ Reserve(TTL) [orphan admitted cleanup] → Issue(nickName) → token + RoomSummary
```

Политика A: peer в room → `ErrPeerAlreadyInRoom`; слот held → `ErrTicketSlotHeld`.

## JoinRoom

```text
Validate → GetRoom → Admit → CreatePeer(..., claims.NickName)
→ Replace (если HasPeer) | Join → Start
```

Откат: при ошибке до успешного join в room — `Revoke` (кроме failed Replace: старый peer остаётся).  
При `Start` failure — `Leave`, `Stop`, `Revoke`.

## LeaveRoom

`GetPeer` → `Leave` → `Stop` → `Revoke`. Идемпотентно: комната или peer уже сняты → `Revoke` + `nil`; слот reservation отсутствует → `nil`.

## Delete

```text
VerifyRoomPassword (только Delete) → GetRoom → kick all peers (LeaveRoom)
→ DeleteRoom → UnregisterRoom
```

Ошибки kick логируются; комната всё равно удаляется. `DeleteForShutdown` — без пароля, тот же kick.

## EvictIdlePeers

Периодический sweep (`StartIdlePeerSweep`): `registry.GetList` → `room.GetPeers` → `!peer.IsAlive()` → `LeaveRoom` (`Leave` → `Stop` → `Revoke`, молчаливый `Close`). Ошибка одного peer логируется, sweep продолжается. `interval <= 0` — no-op; таймаут живости — внутри `WsConnection` (`TransportConfig.PeerIdleTimeout`).

## HTTP / transport

Вызываются из `delivery/http`, `delivery/ws`, `delivery/enet` — см. их README.
