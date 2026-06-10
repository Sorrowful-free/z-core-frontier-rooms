# cmd/rooms

Composition root: DI, Fiber `:3000`, ENet `7777` (при `-tags enet`).

```text
fiber.New() + zap middleware
  → StateRoomPolicyFactory, RoomFactory, RoomRegistry, PeerFactory
  → admission (dev secret, TTL 1h), reservation (in-memory)
  → Create, IssueTicket, JoinRoom, LeaveRoom, Delete, GetList
  → http / ws RegisterRoutes
  → enet.Listen() (goroutine)
  → app.Listen(":3000")
```

Shutdown: SIGINT → `GetList` → `Delete` по комнатам → `registry.Shutdown`.

См. [docs/architecture.md](../../docs/architecture.md).
