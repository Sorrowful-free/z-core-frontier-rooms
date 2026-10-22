# P6 — Audit remediation (index)

## Контекст

Полный аудит (6 осей: domain/usecase, realtime+codec, delivery+transport, config/infra, tests, docs-vs-code). Тулчейн чистый — все находки логические. План раздроблен на независимые куски для передачи агентам; каждый чанк — self-contained план со своим скоупом.

## Решения (зафиксировано с пользователем)

- **D1**: `ADMISSION_PASSWORD` **удалить полностью** — HTTP API key уже гейтит control plane.
- **D2**: `Password` **выкинуть из wire** — поле из `RoomState`/`RoomStatePatch`/codec/wire-docs. Breaking change: синхронный апдейт Godot-клиента. **Пароль комнаты при этом остаётся** — он серверный секрет в `reservation` (хранится при создании, проверяется на issue/delete); в P6b добавлено хэширование хранимого пароля.
- **D3**: тикет **one-time-use** — повторный `Admit` на admitted-слоте → `ErrReservationAlreadyAdmitted`. Реконнект = новый тикет/peerID, старый peer выметает idle-sweep (ADR-0001).
- **D4**: `Room.Leave` **удаляет из `r.peers` всегда** — ошибка политики логируется/возвращается, peer считается вышедшим.
- **D5**: дедup тикетов — **1 outstanding `reserved`-слот на (roomID, nickName)**; `admitted` не блокирует.
- **D6**: `Room.Send` broadcast-abort — решено в P4, не переоткрываем.
- **ADR-0004** `ticket-lifecycle-semantics` — пишется в P6e.

## Карта чанков

| Чанк | Тема | Файлы-скоуп | Зависит от |
|------|------|-------------|------------|
| [P6a](P6a-lifecycle-atomicity.md) | Lifecycle atomicity | `realtime/room.go`, `policy/state/{server,client}_*`, `usecase/{leave_room,join_room}` | — |
| [P6b](P6b-ticket-semantics.md) | Тикеты: D1+D3+D5, capacity, reservation races | `port/reservation`, `adapter/{reservation,admission,config}`, `usecase/{issue_ticket,create,join_room}`, `delivery/errors`, `main.go` (admission wiring) | **P6a** (join_room.go общий) |
| [P6c](P6c-http-infra-hardening.md) | HTTP/infra: rate limits, recover, registry, sweep-handles, stdlib logger | `delivery/http`, `httplimits`, `registry`, `usecase/{evict,sweep}`, `logging/stdlib`, `main.go` (recover+wait) | — |
| [P6d](P6d-wire-password-removal.md) | D2: Password из wire + codec hygiene | `domain/state/room_state.go`, `codec/*`, wire-docs | — |
| [P6e](P6e-tests-docs-adr.md) | Тесты, docs-фиксы, ADR-0004, статусы P1–P5 | `tests/**`, docs | **P6a–P6d** (тесты под новую семантику) |
| **P7** (план отдельно) | ENet-волна | `{adapter/transport,delivery}/enet/**` | после P6 |

Параллелизм: P6a ∥ P6c ∥ P6d — файлы не пересекаются. P6b ждёт P6a (оба трогают `join_room.go`). P6e — последним.

## Общие проверки (каждый чанк)

```bash
go build ./... && go vet ./... && ~/go/bin/golangci-lint run ./...
go test -race -shuffle=on -count=1 ./tests/...
```

## Глобальное «не трогать»

- `internal/{adapter/transport,delivery}/enet/**` — P7.
- `Room.Send` abort-семантика — решение P4.
- `codec/**` — только под P6d.
- `main.go` — P6b трогает только admission-wiring (D1), P6c — только recover/sweep-wait.
