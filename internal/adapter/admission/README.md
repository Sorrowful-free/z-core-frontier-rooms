# adapter/admission

HMAC-SHA256 ticket с TTL. Порт: `port/admission`.

## API

- `NewAdmission(cfg AdmissionConfig)` — конструктор
- `AdmissionConfig.Validate()` — валидация перед стартом
- `Issue(ctx, roomID, peerID, nickName, password)` → `[]byte`
- `Validate(ctx, token)` → `domain.Claims`
- `TTL()` — длительность брони/issue

Env и загрузка — [adapter/config/README.md](../config/README.md).

## Ticket v2 (бинарный)

```text
version(1) | roomID(8) | peerID(8) | issuedAt(8) | expiresAt(8) | nickLen(1) | nick | HMAC-SHA256(32)
```

Размер: `67 + len(nick)` … `130` байт (`nick` 1…64 байта, `domain.ValidateNickName`).

`password` в `AdmissionConfig`: если не пустой — проверяется при Issue.

## Ошибки

`ErrInvalidCredentials`, `ErrInvalidToken`, `ErrExpiredToken` (+ `domain.ErrInvalidNickName` при Issue).
