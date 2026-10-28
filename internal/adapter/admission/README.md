# adapter/admission

HMAC-SHA256 ticket with TTL. Port: `port/admission`.

## API

- `NewAdmission(cfg AdmissionConfig)` — constructor
- `AdmissionConfig.Validate()` — validation before start
- `Issue(ctx, roomID, peerID, nickName, hostID)` → `[]byte` (join ticket, kind `1`)
- `IssueReconnect(ctx, roomID, peerID, hostID)` → `[]byte` (reconnect token, kind `2`, P20)
- `Validate(ctx, token)` → `domain.Claims` (including `Claims.HostID`, P18 and `Claims.Kind`, P20)
- `TTL()` — reservation/issue duration

Env and loading — [adapter/config/README.md](../config/README.md).

The first byte of the credential is `kind`: `1` = join ticket, `2` = reconnect token. `Validate` dispatches parsing on it.

## Join ticket (kind `1`, binary)

```text
kind(1)=1 | roomID(8) | peerID(8) | issuedAt(8) | expiresAt(8) | hostID(2) | nickLen(1) | nick | HMAC-SHA256(32)
```

Size: `68 + len(nick)` … `132` bytes (`nick` 1…64 bytes, `domain.ValidateNickName`; fixed part 36 + MAC 32).

`hostID` — assigned ENet host (shard) for strict binding admit (P18); `0` (`domain.HostIDUnbound`) — unbound: the ticket is issued without `HostRegistry` (WS-only deploy) and is rejected with `ErrWrongHost` on ENet admit. Issue calls `IssueTicketUseCase` after `AssignHost` on `port/transport/enet.HostRegistry`.

## Reconnect token (kind `2`, P20)

```text
kind(1)=2 | roomID(8) | peerID(8) | hostID(2) | HMAC-SHA256(32)
```

Fixed size 51 bytes; no `nickName` (on resume the nick is taken from the reservation slot) and no timestamps — the token has no expiry; the grace window (`lastSeen + RESERVATION_RECONNECT_TTL`) is counted by the server against the admitted slot. `Validate` sets `claims.Kind = ClaimsKindReconnect` — the admit facade splits join/resume by Kind; `hostID` inherits the original ticket's binding (strict binding admit is preserved).

The room password is not checked at Issue — it is verified by `VerifyRoomPassword` in `IssueTicketUseCase` before `Reserve`. The ticket is single-use: a repeated `Admit` on the same slot is rejected (`ErrReservationAlreadyAdmitted`); return after a disconnect — via reconnect token (kind `2`).

## Errors

`ErrInvalidCredentials`, `ErrInvalidToken`, `ErrExpiredToken` (+ `domain.ErrInvalidNickName` on Issue).
