# cmd/rooms

Composition root: DI, Fiber `:3000`, admin Fiber `ADMIN_LISTEN_ADDR` (opt-in), ENet `7777` (with `-tags enet`).

```text
fiber.New() + metrics middleware + zap middleware
  → metrics.New(cfg.Metrics) → port/metrics into all emitters
  → StateRoomPolicyFactory, RoomFactory, RoomRegistry, PeerFactory
  → config.LoadFromEnv() → admission, httplimits.New(cfg.HTTPLimits), reservation (in-memory)
  → Create, IssueTicket, JoinRoom, LeaveRoom, Delete, GetList
  → http / ws RegisterRoutes
  → enet.Listen() (goroutine)
  → app.Listen(":3000")
  → net.Listen(ADMIN_LISTEN_ADDR) synchronously, then admin.NewMetricsApp(gatherer).Listener(ln) in a goroutine, if the address is set
```

Admin listener binds synchronously before the serve goroutine (P19b): bind failure - fail-fast `run`, the "shutdown before listen" race is ruled out.

Shutdown: SIGINT → `ShutdownWithContext` of both Fiber apps (admin has its own timeout-ctx) → `GetList` → `Delete` per room → `registry.Shutdown`. Any listener failing (HTTP, admin, ENet) — fail-fast the process.

## Env

`ADMISSION_SECRET` (≥ 32 bytes) and `HTTP_API_KEY` (≥ 16 bytes) are required, or `HTTP_AUTH_DISABLED=true` for dev. See [.env.example](../../.env.example), [internal/adapter/config/README.md](../../internal/adapter/config/README.md).

See [docs/architecture.md](../../docs/architecture.md).
