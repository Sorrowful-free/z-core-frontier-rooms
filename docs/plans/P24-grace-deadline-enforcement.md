# P24 — enforce grace deadline на resume

## Контекст

Аудит P20: expiry grace реализовано **только** через
`SweepOrphanAdmittedUseCase`. Resume после `lastSeen + RECONNECT_TTL`,
но до прохода sweep — успешен; при `RESERVATION_ORPHAN_SWEEP_INTERVAL=0`
grace бессмертен (reconnect-токен вечный, пока жив слот).
Загриллено: enforce на resume-потоке.

Механический нюанс: `touchAdmitted` сразу перезаписывает
`admittedAt = now` и возвращает освежённый слот — UC не видит старый
lastSeen. Разнесённая проверка без защиты от refresh'а была бы
дырявой: отклонённая попытка сама продлевает grace (перезаписывает
lastSeen), и повторная уже проходит. Поэтому загриллен вариант A2 —
**read-only** метод + проверка до мутации.

## Решения (загриллены)

### Read-only слот в порту reservation

`port/reservation`: новый метод

```go
// SlotDetails — read-only просмотр admitted-слота (P24): возвращает
// слот БЕЗ refresh'а admittedAt (в отличие от Reconnect) — источник
// lastSeen для проверки grace-дедлайна. Слота нет или не admitted →
// ErrReservationNotReserved.
SlotDetails(ctx context.Context, roomID domain.RoomID, peerID domain.PeerID) (domain.AdmittedSlot, error)
```

Адаптер: выделить неизменяющую часть `touchAdmitted` (lookupRoom +
room.mu + checkLive + admitted-проверка) — `SlotDetails` читает под
тем же mutex, не мутирует. Имя метода — `SlotDetails` (альтернатива
`AdmittedPeer` — при реализации выбрать читаемое, порт-комментарий
фиксирует контракт).

### Проверка дедлайна в ReconnectRoomUseCase

Поток resume становится:

1. `GetRoom(claims.RoomID)`;
2. `reservation.SlotDetails(roomID, peerID)` — честный lastSeen
   (pre-refresh); слота нет/не admitted → `ErrReservationNotReserved`
   → reject 0x73;
3. **deadline-гейт**: `if !room.HasPeer(peerID) &&
   now.After(slot.AdmittedAt + reconnectTTL) → ErrReservationExpired`
   → reject 0x74 (маппинг уже есть в
   `delivery/errors/reservation_reject.go`). Живой peer (`HasPeer`) —
   newest-wins Replace без гейта: admittedAt у давно подключённого
   peer'а старый законно, дедлайн на него не распространяется;
4. `reservation.Reconnect` — как раньше (admitted-проверка + refresh
   под room.mu; гонка sweep↔resume закрыта сериализацией; проигравший
   sweep'у resume получает NotReserved → 0x73);
5. `placePeer` → broadcast 0x81 → `ReconnectSucceeded`.

`ReconnectRoomUseCase` получает `reconnectTTL time.Duration` в
конструктор (из `cfg.Reservation.ReconnectTTL`, cmd/rooms). Свойство
«отклонённая попытка не продлевает grace»: reject случается ДО
`Reconnect` → lastSeen не трогается → повторные попытки тоже видят
expired → sweep revoke освобождает слот.

Граничный случай: `reconnectTTL <= 0` → любой `!HasPeer` resume
expired (тот же ноль-семантизм, что у sweep: «grace нет»); при живом
peer'е Replace по-прежнему доступен — TTL=0 запрещает именно
воскрешение, не перехват соединения. Зафиксировать в комментарии UC.

### Отклонённые альтернативы (не реализовывать)

- Флаг-параметр в `Reconnect` (`enforceDeadline bool`): меньший diff,
  но flag-аргумент + check/refresh смешаны; отклонено гриллингом.
- Join `markErr` в `DisconnectPeer` return / подавление 0x80 при фейле
  mark'а: отклонено — mark best-effort, 0x80 единственный сигнал
  выхода peer'а, Error-лог достаточен.

## Скоуп правок (фиксированный)

- `internal/port/reservation/reservation.go` — метод `SlotDetails`.
- `internal/adapter/reservation/reservation.go` — реализация
  (неизменяющий вариант touchAdmitted-пути), комментарии.
- `internal/usecase/room/reconnect_room.go` — `reconnectTTL` в
  конструктор, шаги 2–3.
- `cmd/rooms/main.go` — передача `cfg.Reservation.ReconnectTTL`.
- `tests/mocks/` — перегенерация (`go generate ./tests/mocks/...`).
- `tests/adapter/reservation/` — SlotDetails: admitted-слот читается
  без refresh'а (admittedAt не меняется повторным чтением); missing/
  reserved → NotReserved.
- `tests/usecase/room/` — expired-grace resume → `ErrReservationExpired`
  (0x74 на wire-уровне проверяется mapping'ом — уже покрыт);
  резюме живого peer'а (HasPeer) дедлайном не гейтится;
  `reconnectTTL=0` → !HasPeer resume всегда expired.
- Docs по package-docs: `internal/adapter/reservation/README.md`,
  `internal/usecase/room/README.md`, `internal/port/reservation`
  (doc-комментарий порта), `internal/delivery/errors/README.md` —
  строчка про 0x74 на resume. Короткий `docs/adr/0009-grace-deadline.md`:
  enforce на resume + порядок «check до refresh» + живой-peer exemption.
- Client-protocol абзац — частью P21(c), отдельно.

## Верификация (для сабагента)

- `go build ./...` — 0 ошибок.
- `go vet ./...` — 0.
- `~/go/bin/golangci-lint run ./...` — 0 issues.
- `go generate ./tests/mocks/...` — моки перегенерированы, diff чистый
  после сборки.
- `go test -race -shuffle=on -short -count=1 ./tests/...` — зелёные,
  включая новые кейсы выше.
