# adapter/httplimits

Лимиты control plane: rate limit (per IP) и ёмкость инстанса.

Порт: `port/httplimits.Limits`. Реализация: `New(HTTPLimitsConfig)`.

## HTTPLimitsConfig

| Поле | Env | Default | `0` |
|------|-----|---------|-----|
| `MaxRooms` | `MAX_ROOMS` | `500` | без лимита |
| `CreateRoomsPerMinute` | `HTTP_CREATE_RATE_PER_MIN` | `30` | без лимита |
| `IssueTicketsPerMinute` | `HTTP_ISSUE_RATE_PER_MIN` | `60` | без лимита |

## Limits (port)

| Метод | Где |
|-------|-----|
| `AllowCreateRoom(count)` | `usecase/room.Create` |
| `AllowHTTPCreate(ip)` | middleware `POST /rooms` |
| `AllowHTTPIssueTicket(ip)` | middleware `POST /rooms/:id/tickets` |

Env — [adapter/config/README.md](../config/README.md).
