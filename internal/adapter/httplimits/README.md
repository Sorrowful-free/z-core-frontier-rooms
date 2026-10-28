# adapter/httplimits

Control plane limits: rate limit (per IP) and instance capacity.

Port: `port/httplimits.Limits`. Implementation: `New(HTTPLimitsConfig)`.

## HTTPLimitsConfig

| Field | Env | Default | `0` |
|-------|-----|---------|-----|
| `MaxRooms` | `MAX_ROOMS` | `500` | no limit |
| `CreateRoomsPerMinute` | `HTTP_CREATE_RATE_PER_MIN` | `30` | no limit |
| `IssueTicketsPerMinute` | `HTTP_ISSUE_RATE_PER_MIN` | `60` | no limit |
| `DeleteRoomsPerMinute` | `HTTP_DELETE_RATE_PER_MIN` | `30` | no limit |
| `ListRoomsPerMinute` | `HTTP_LIST_RATE_PER_MIN` | `60` | no limit |

## Limits (port)

| Method | Where |
|--------|-------|
| `AllowCreateRoom(count)` | `usecase/room.Create` |
| `AllowHTTPCreate(ip)` | `POST /rooms` middleware |
| `AllowHTTPIssueTicket(ip)` | `POST /rooms/:id/tickets` middleware |
| `AllowHTTPDelete(ip)` | `DELETE /rooms/:id` middleware |
| `AllowHTTPList(ip)` | `GET /rooms` middleware |

## Rate limiter

Fixed window of 1 min per key (`<scope>:<ip>`): counter up to limit, the window resets at `resetAt`.
A burst of up to 2×limit is possible at a window boundary — a known property of fixed window.

- The key is the client's `c.IP()`: behind a reverse proxy this is the proxy IP unless Fiber is configured with `ProxyHeader`/`TrustedProxies` — then the limit is shared across the proxy.
- Expired windows are cleaned up lazily: inside `allow`, no more often than `rateLimitSweepInterval`, without a separate goroutine — `windows` growth under IP rotation is bounded.

Env — [adapter/config/README.md](../config/README.md).
