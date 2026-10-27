# adapter/admission

HMAC-SHA256 ticket с TTL. Порт: `port/admission`.

## API

- `NewAdmission(cfg AdmissionConfig)` — конструктор
- `AdmissionConfig.Validate()` — валидация перед стартом
- `Issue(ctx, roomID, peerID, nickName, hostID)` → `[]byte` (join-ticket, kind `1`)
- `IssueReconnect(ctx, roomID, peerID, hostID)` → `[]byte` (reconnect-токен, kind `2`, P20)
- `Validate(ctx, token)` → `domain.Claims` (включая `Claims.HostID`, P18 и `Claims.Kind`, P20)
- `TTL()` — длительность брони/issue

Env и загрузка — [adapter/config/README.md](../config/README.md).

Первый байт credential'а — `kind`: `1` = join-ticket, `2` = reconnect-токен. `Validate` диспатчит разбор по нему.

## Join-ticket (kind `1`, бинарный)

```text
kind(1)=1 | roomID(8) | peerID(8) | issuedAt(8) | expiresAt(8) | hostID(2) | nickLen(1) | nick | HMAC-SHA256(32)
```

Размер: `68 + len(nick)` … `132` байт (`nick` 1…64 байта, `domain.ValidateNickName`; фиксированная часть 36 + MAC 32).

`hostID` - назначенный ENet host (shard) для strict binding admit (P18); `0` (`domain.HostIDUnbound`) - без привязки: ticket выписывается без `HostRegistry` (WS-only deploy) и на ENet-admit отклоняется `ErrWrongHost`. Issue вызывает `IssueTicketUseCase` после `AssignHost` у `port/transport/enet.HostRegistry`.

## Reconnect-токен (kind `2`, P20)

```text
kind(1)=2 | roomID(8) | peerID(8) | hostID(2) | HMAC-SHA256(32)
```

Фиксированный размер 51 байт; без `nickName` (ник при resume берётся из слота reservation) и без timestamps — expiry у токена нет, grace окно (`lastSeen + RESERVATION_RECONNECT_TTL`) отсчитывает сервер по admitted-слоту. `Validate` ставит `claims.Kind = ClaimsKindReconnect` — admit-фасад разводит join/resume по Kind; `hostID` наследует привязку исходного тикета (strict binding admit сохраняется).

Пароль комнаты при Issue не проверяется — его сверяет `VerifyRoomPassword` в `IssueTicketUseCase` до `Reserve`. Тикет одноразовый: повторный `Admit` на том же слоте отклоняется (`ErrReservationAlreadyAdmitted`); возврат после разрыва — reconnect-токеном (kind `2`).

## Ошибки

`ErrInvalidCredentials`, `ErrInvalidToken`, `ErrExpiredToken` (+ `domain.ErrInvalidNickName` при Issue).
