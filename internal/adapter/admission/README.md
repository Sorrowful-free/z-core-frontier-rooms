# adapter/admission

HMAC-SHA256 ticket с TTL. Порт: `port/admission`.

## API

- `NewAdmission(cfg AdmissionConfig)` — конструктор
- `AdmissionConfig.Validate()` — валидация перед стартом
- `Issue(ctx, roomID, peerID, nickName, hostID)` → `[]byte` (join-ticket v3)
- `IssueReconnect(ctx, roomID, peerID, hostID)` → `[]byte` (reconnect-токен v4, P20)
- `Validate(ctx, token)` → `domain.Claims` (включая `Claims.HostID`, P18 и `Claims.Kind`, P20)
- `TTL()` — длительность брони/issue

Env и загрузка — [adapter/config/README.md](../config/README.md).

## Ticket v3 (бинарный)

```text
version(1)=3 | roomID(8) | peerID(8) | issuedAt(8) | expiresAt(8) | hostID(2) | nickLen(1) | nick | HMAC-SHA256(32)
```

Размер: `68 + len(nick)` … `132` байт (`nick` 1…64 байта, `domain.ValidateNickName`; фиксированная часть 36 + MAC 32).

`hostID` - назначенный ENet host (shard) для strict binding admit (P18); `0` (`domain.HostIDAny`) - без привязки, admit на любом host'е. Issue вызывает `IssueTicketUseCase` после `AssignHost` у `port/transport/enet.HostRegistry`.

**Ticket v2 (legacy)** - тот же layout без поля `hostID` (version byte `2`, фиксированная часть 34): `Verify` принимает обе версии, у v2 `claims.HostID = HostIDAny`. Обратная совместимость со старыми тикетами сохраняется.

## Ticket v4 — reconnect-токен (P20)

```text
version(1)=4 | roomID(8) | peerID(8) | hostID(2) | HMAC-SHA256(32)
```

Фиксированный размер 51 байт; без `nickName` (ник при resume берётся из слота reservation) и без timestamps — expiry у токена нет, grace окно (`lastSeen + RECONNECT_TTL`) отсчитывает сервер по admitted-слоту. `Validate` ставит `claims.Kind = ClaimsKindReconnect` — admit-фасад разводит join/resume по Kind; `hostID` наследует привязку исходного тикета (strict binding admit сохраняется).

Пароль комнаты при Issue не проверяется — его сверяет `VerifyRoomPassword` в `IssueTicketUseCase` до `Reserve`. Тикет одноразовый: повторный `Admit` на том же слоте отклоняется (`ErrReservationAlreadyAdmitted`); возврат после разрыва — reconnect-токеном v4.

## Ошибки

`ErrInvalidCredentials`, `ErrInvalidToken`, `ErrExpiredToken` (+ `domain.ErrInvalidNickName` при Issue).
