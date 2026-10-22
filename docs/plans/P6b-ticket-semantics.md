# P6b — Ticket semantics (D1, D3, D5) + capacity + reservation races

> **Статус:** done (verified: build/vet/lint clean, go test -race -shuffle=on green).

## Контекст

Три зафиксированных решения по семантике тикетов плюс дыры аудита вокруг reservation: slot-flood (нет дедup — каждый тикет = свежий peerID), `ADMISSION_PASSWORD` коллизия, plaintext-хранение пароля комнаты, TOCTOU в `lookupRoom` и `AllowCreateRoom`, отсутствие capacity-валидации ниже HTTP-слоя.

## Зависит от

**P6a** — оба чанка трогают `internal/usecase/room/join_room.go` (P6a: rollback-логи; P6b: маппинг `ErrReservationAlreadyAdmitted`).

## Решения (D1, D3, D5)

- `ADMISSION_PASSWORD` удалить из `Admission.Issue`, `AdmissionConfig`, `load.go`, `.env.example`, docs.
- `Admit` идемпотентность убрать: повторный admit на admitted-слоте → `ErrReservationAlreadyAdmitted` (sentinel уже в domain — оживает).
- `peerSlot` + `nickName`: `Reserve` → `ErrTicketSlotHeld` если в комнате есть **reserved** (не admitted) слот с тем же nick. Scan O(capacity≤127) — ок.
- Пароль комнаты: хранить хэш (`sha256`+salt или `crypto/hmac` от secret), сверка через `subtle.ConstantTimeCompare`.

## Скоуп правок (override красной зоны)

- `internal/port/reservation/reservation.go` — сигнатура `Reserve(+nickName)`
- `internal/adapter/reservation/*.go`
- `internal/adapter/admission/*.go`
- `internal/adapter/config/load.go` (+ `config/README.md`)
- `internal/usecase/room/{issue_ticket,create,join_room}.go`
- `internal/delivery/errors/*.go` (проверить маппинг `ErrReservationAlreadyAdmitted`/`ErrTicketSlotHeld` → OpCode; добавить если нет)
- `internal/domain/**` — только если нужен новый sentinel
- `cmd/rooms/main.go` — **только** admission-wiring (D1)
- `.env.example`, README: admission, config, reservation, delivery/errors, client-protocol `errors.md`/`http.md`
- `tests/**` соответствующих пакетов, `tests/mocks/*reservation*`

## Не трогать

- `room.go`, policy — P6a. `rate_limiter`, `rooms.go` — P6c. `codec` — P6d.

## Шаги

1. `admission.go`/`admission_config.go`: убрать `password` поле, проверку в `Issue`, `ADMISSION_PASSWORD` из `load.go` + `.env.example`. Порт `admission.Admission.Issue` — сигнатура без `password` (проверить все вызовы: `issue_ticket.go:85`, моки, тесты).
2. `reservation.go` `Admit`: `peer.state == admitted` → `ErrReservationAlreadyAdmitted` вместо `nil` (строки 164-166). `join_room.go`: ошибка Admit → существующий поток в `SendJoinReject` — проверить что `mapReservationRejectOpCode` покрывает sentinel; нет — добавить.
3. `reservation.go`: `peerSlot.nickName`; `Reserve(ctx, roomID, peerID, nickName, expiresAt)` — rejected с `ErrTicketSlotHeld` если ∃ reserved-слот с таким nick (admitted не считается). Port + все вызовы + моки обновить.
4. `reservation.go`: `passwordHash` вместо `password`; `RegisterRoom` хэширует; `VerifyRoomPassword` → `subtle.ConstantTimeCompare`. Пустой пароль — как сейчас (no-op).
5. `reservation.go` `lookupRoom` TOCTOU: tombstone-flag на `reservationRoom` при `UnregisterRoom` или сверка членства под `roomsMu` до `room.mu.Lock()`.
6. `create.go`: TOCTOU `AllowCreateRoom(len(rooms))`+`CreateRoom` — под общий семафор/лок usecase; валидация `capacity` `1..domain.MaxRoomCapacity` в `Create` и `RegisterRoom` (fail-fast, не только HTTP parse).
7. `issue_ticket.go`: после D5 — `mapIssueReserveError` оживает (реальный `ErrTicketSlotHeld`); orphan-retry ветку оставить (случает после sweep-gonки) или упростить — по факту покрытия тестами.

## Тесты

- `Admit` дважды → `ErrReservationAlreadyAdmitted`; join с использованным тикетом → reject-OpCode `0x70+`.
- `Reserve` дважды с тем же nick (разные peerID) → второй `ErrTicketSlotHeld`; после `Admit` первого — второй Reserve с тем же nick **ok** (reconnect).
- `VerifyRoomPassword` wrong/right; хэш ≠ plaintext в структуре.
- `IssueTicket` без `ADMISSION_PASSWORD` env — путь чистый; `create` capacity 0/128 → ошибка.
- Конкурентный `CreateRoom`×2 превышающий лимит → один проходит.
- Существующие admission/reservation/issue_ticket/join тесты — зелёные (сигнатуры обновить).

## Проверки

```bash
go build ./... && go test -race -count=3 ./tests/adapter/reservation/... ./tests/adapter/admission/... ./tests/usecase/room/... ./tests/delivery/errors/...
```
