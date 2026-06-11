# cmd/rooms

Composition root: DI, Fiber `:3000`, ENet `7777` (при `-tags enet`).

```text
fiber.New() + zap middleware
  → StateRoomPolicyFactory, RoomFactory, RoomRegistry, PeerFactory
  → config.LoadFromEnv() → admission, httplimits.New(cfg.HTTPLimits), reservation (in-memory)
  → Create, IssueTicket, JoinRoom, LeaveRoom, Delete, GetList
  → http / ws RegisterRoutes
  → enet.Listen() (goroutine)
  → app.Listen(":3000")
```

Shutdown: SIGINT → `GetList` → `Delete` по комнатам → `registry.Shutdown`.

## Env

Обязательны `ADMISSION_SECRET` (≥ 32 байт) и `HTTP_API_KEY` (≥ 16 байт), либо `HTTP_AUTH_DISABLED=true` для dev. См. [.env.example](../../.env.example), [internal/adapter/config/README.md](../../internal/adapter/config/README.md).

См. [docs/architecture.md](../../docs/architecture.md).
