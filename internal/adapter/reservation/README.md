# adapter/reservation

In-memory слоты комнаты. Порт: `port/reservation`.

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

- `Revoke` без peer в map → `nil` (идемпотентно).
- Просроченные `reserved` — lazy sweep при `Reserve`.
- `admitted` хранит `admittedAt`; orphan (нет peer в room) — фоновый sweep (`SweepOrphanAdmittedUseCase`) после `OrphanAdmittedTTL`.
- Env: `RESERVATION_ORPHAN_ADMITTED_TTL` (default `30s`), `RESERVATION_ORPHAN_SWEEP_INTERVAL` (default `10s`; `0` — отключить).

Ошибки: `domain/reservation_error.go`. Wire `0x70–0x7F` — `delivery/errors`.
