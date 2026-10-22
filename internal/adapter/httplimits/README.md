# adapter/httplimits

Лимиты control plane: rate limit (per IP) и ёмкость инстанса.

Порт: `port/httplimits.Limits`. Реализация: `New(HTTPLimitsConfig)`.

## HTTPLimitsConfig

| Поле | Env | Default | `0` |
|------|-----|---------|-----|
| `MaxRooms` | `MAX_ROOMS` | `500` | без лимита |
| `CreateRoomsPerMinute` | `HTTP_CREATE_RATE_PER_MIN` | `30` | без лимита |
| `IssueTicketsPerMinute` | `HTTP_ISSUE_RATE_PER_MIN` | `60` | без лимита |
| `DeleteRoomsPerMinute` | `HTTP_DELETE_RATE_PER_MIN` | `30` | без лимита |
| `ListRoomsPerMinute` | `HTTP_LIST_RATE_PER_MIN` | `60` | без лимита |

## Limits (port)

| Метод | Где |
|-------|-----|
| `AllowCreateRoom(count)` | `usecase/room.Create` |
| `AllowHTTPCreate(ip)` | middleware `POST /rooms` |
| `AllowHTTPIssueTicket(ip)` | middleware `POST /rooms/:id/tickets` |
| `AllowHTTPDelete(ip)` | middleware `DELETE /rooms/:id` |
| `AllowHTTPList(ip)` | middleware `GET /rooms` |

## Rate limiter

Fixed window 1 мин на ключ (`<scope>:<ip>`): счётчик до limit, окно сбрасывается по `resetAt`.
На границе окон возможен burst до 2×limit — известное свойство fixed window.

- Ключ — `c.IP()` клиента: за reverse-proxy это IP прокси, если Fiber не настроен с `ProxyHeader`/`TrustedProxies` — тогда лимит общий на прокси.
- Просроченные окна вычищаются lazy: внутри `allow` не чаще `rateLimitSweepInterval`, без отдельной горутины — рост `windows` при IP-ротации ограничен.

Env — [adapter/config/README.md](../config/README.md).
