# delivery

Transport and wire mapping. Depends on `usecase`, not on `adapter` directly (except composition in `cmd`).

| Package | README |
|---------|--------|
| `http/` | [http/README.md](http/README.md) — REST control plane |
| `ws/` | [ws/README.md](ws/README.md) — WebSocket data plane |
| `enet/` | [enet/README.md](enet/README.md) — ENet UDP |
| `errors/` | [errors/README.md](errors/README.md) — OpCode join / in-room / reservation |
| `admin/` | [admin/README.md](admin/README.md) — admin listener `GET /metrics` (separate Fiber instance) |

Handlers are thin: parse → use case → response or wire error.
