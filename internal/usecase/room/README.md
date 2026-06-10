# usecase/room

Сценарии комнаты. Зависит от `port/*`, не от `delivery` и не от конкретных adapter.

## Use case

| Файл | Плоскость | Кратко |
|------|-----------|--------|
| `create.go` | Control | `RegisterRoom` → registry; откат `UnregisterRoom` |
| `delete.go` | Control | `DeleteRoom` → `UnregisterRoom` |
| `get_list.go` | Control | Список комнат |
| `issue_ticket.go` | Control | `ValidateNickName` → Reserve → Issue |
| `join_room.go` | Data | Validate → Admit → CreatePeer(nick) → Join/Replace → Start |
| `leave_room.go` | Data | Leave → Stop → Revoke |

## IssueTicket

```text
ValidateNickName → GetRoom → HasPeer? → VerifyRoomPassword
→ Reserve(TTL) [orphan admitted cleanup] → Issue(nickName) → token
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

`GetPeer` → `Leave` → `Stop` → `Revoke` (идемпотентно при отсутствии слота).

## HTTP / transport

Вызываются из `delivery/http`, `delivery/ws`, `delivery/enet` — см. их README.
