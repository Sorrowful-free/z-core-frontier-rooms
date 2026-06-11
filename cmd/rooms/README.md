# cmd/rooms

Composition root: DI, Fiber `:3000`, ENet `7777` (при `-tags enet`).

```text
fiber.New() + zap middleware
  → StateRoomPolicyFactory, RoomFactory, RoomRegistry, PeerFactory
  → config.LoadFromEnv() → admission.NewAdmission(cfg.Admission), reservation (in-memory)
  → Create, IssueTicket, JoinRoom, LeaveRoom, Delete, GetList
  → http / ws RegisterRoutes
  → enet.Listen() (goroutine)
  → app.Listen(":3000")
```

Shutdown: SIGINT → `GetList` → `Delete` по комнатам → `registry.Shutdown`.

## Env

Обязателен `ADMISSION_SECRET` (≥ 32 байт). См. [.env.example](../../.env.example), [internal/adapter/config/README.md](../../internal/adapter/config/README.md).

См. [docs/architecture.md](../../docs/architecture.md).
