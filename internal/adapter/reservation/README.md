# adapter/reservation

In-memory слоты комнаты. Порт: `port/reservation`.

## Методы

| Метод | Кто вызывает |
|-------|----------------|
| `RegisterRoom` / `UnregisterRoom` | Create / Delete |
| `Reserve` | IssueTicket |
| `Admit` | JoinRoom |
| `Revoke` | LeaveRoom, откат Issue/Join, orphan cleanup в Issue |
| `State` | IssueTicket (orphan) |
| `VerifyRoomPassword` | IssueTicket, Delete |

## Слоты

`none` → `reserved` (TTL из admission) → `admitted` (до Leave / orphan cleanup).

- `Revoke` без peer в map → `nil` (идемпотентно).
- Просроченные `reserved` — lazy sweep при `Reserve`.
- `admitted` без TTL до Leave или orphan revoke в Issue.

Ошибки: `domain/reservation_error.go`. Wire `0x70–0x7F` — `delivery/errors`.
