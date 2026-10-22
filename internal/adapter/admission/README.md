# adapter/admission

HMAC-SHA256 ticket с TTL. Порт: `port/admission`.

## API

- `NewAdmission(cfg AdmissionConfig)` — конструктор
- `AdmissionConfig.Validate()` — валидация перед стартом
- `Issue(ctx, roomID, peerID, nickName)` → `[]byte`
- `Validate(ctx, token)` → `domain.Claims`
- `TTL()` — длительность брони/issue

Env и загрузка — [adapter/config/README.md](../config/README.md).

## Ticket v2 (бинарный)

```text
version(1) | roomID(8) | peerID(8) | issuedAt(8) | expiresAt(8) | nickLen(1) | nick | HMAC-SHA256(32)
```

Размер: `66 + len(nick)` … `130` байт (`nick` 1…64 байта, `domain.ValidateNickName`; фиксированная часть 34 + MAC 32).

Пароль комнаты при Issue не проверяется — его сверяет `VerifyRoomPassword` в `IssueTicketUseCase` до `Reserve`. Тикет одноразовый: повторный `Admit` на том же слоте отклоняется (`ErrReservationAlreadyAdmitted`), реконнект = новый тикет с новым peerID.

## Ошибки

`ErrInvalidCredentials`, `ErrInvalidToken`, `ErrExpiredToken` (+ `domain.ErrInvalidNickName` при Issue).
