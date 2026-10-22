# adapter/reservation

In-memory слоты комнаты. Порт: `port/reservation`.

## Блокировки

- `roomsMu` (`sync.RWMutex`) — только map `roomID → *reservationRoom` (register / unregister / lookup).
- `reservationRoom.mu` — слоты и пароль одной комнаты (`Reserve`, `Admit`, `Revoke`, `State`, `ListAdmittedPeers`, `VerifyRoomPassword`).

Операции в разных комнатах не сериализуют друг друга.

## Методы

| Метод | Кто вызывает |
|-------|----------------|
| `RegisterRoom` / `UnregisterRoom` | Create / Delete |
| `Reserve` | IssueTicket |
| `Admit` | JoinRoom |
| `Revoke` | LeaveRoom, откат Issue/Join, orphan cleanup в Issue, sweep |
| `State` | IssueTicket (orphan) |
| `ListAdmittedPeers` | SweepOrphanAdmitted |
| `VerifyRoomPassword` | IssueTicket, Delete |

## Слоты

`none` → `reserved` (TTL из admission) → `admitted` (до Leave / orphan cleanup).

- `Reserve(ctx, roomID, peerID, nickName, expiresAt)`: один outstanding `reserved`-слот на nick в комнате — повторный Reserve с тем же nick, пока слот не `admitted`/не удалён, → `ErrTicketSlotHeld`.
- `Admit` одноразовый: повторный `Admit` на `admitted`-слоте → `ErrReservationAlreadyAdmitted`.
- `Revoke` без peer в map → `nil` (идемпотентно).
- Просроченные `reserved` — lazy sweep при `Reserve`.
- `admitted` хранит `admittedAt`; orphan (нет peer в room) — фоновый sweep (`SweepOrphanAdmittedUseCase`) после `OrphanAdmittedTTL`.
- Пароль комнаты хранится как SHA-256 (`passwordHash`), сверка — `subtle.ConstantTimeCompare`; plaintext не хранится. `RegisterRoom` валидирует capacity в `[1, domain.MaxRoomCapacity]`.
- Env: `RESERVATION_ORPHAN_ADMITTED_TTL` (default `30s`), `RESERVATION_ORPHAN_SWEEP_INTERVAL` (default `10s`; `0` — отключить).

Бенчмарки (per-room vs legacy global mutex): `tests/adapter/reservation/reservation_bench_test.go`, см. [tests/README.md](../../../tests/README.md).

Ошибки: `domain/reservation_error.go`. Wire `0x70–0x7F` — `delivery/errors`.
