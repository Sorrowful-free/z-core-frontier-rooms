# cmd/rooms

Composition root: DI, Fiber `:3000`, admin Fiber `ADMIN_LISTEN_ADDR` (opt-in), ENet `7777` (при `-tags enet`).

```text
fiber.New() + metrics middleware + zap middleware
  → metrics.New(cfg.Metrics) → port/metrics во все эмиттеры
  → StateRoomPolicyFactory, RoomFactory, RoomRegistry, PeerFactory
  → config.LoadFromEnv() → admission, httplimits.New(cfg.HTTPLimits), reservation (in-memory)
  → Create, IssueTicket, JoinRoom, LeaveRoom, Delete, GetList
  → http / ws RegisterRoutes
  → enet.Listen() (goroutine)
  → app.Listen(":3000")
  → net.Listen(ADMIN_LISTEN_ADDR) синхронно, затем admin.NewMetricsApp(gatherer).Listener(ln) в горутине, если адрес задан
```

Admin listener биндится синхронно до serve-горутины (P19b): bind failure - fail-fast `run`, race "shutdown до listen" исключён.

Shutdown: SIGINT → `ShutdownWithContext` обоих Fiber-приложений (у admin - свой timeout-ctx) → `GetList` → `Delete` по комнатам → `registry.Shutdown`. Падение любого listener'а (HTTP, admin, ENet) — fail-fast процесса.

## Env

Обязательны `ADMISSION_SECRET` (≥ 32 байт) и `HTTP_API_KEY` (≥ 16 байт), либо `HTTP_AUTH_DISABLED=true` для dev. См. [.env.example](../../.env.example), [internal/adapter/config/README.md](../../internal/adapter/config/README.md).

См. [docs/architecture.md](../../docs/architecture.md).
