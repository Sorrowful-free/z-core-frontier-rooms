# z-core-frontier-rooms

Authoritative room/state service for multiplayer matches: **control plane** over HTTP ([Fiber](https://gofiber.io/) v3), **data plane** over ENet UDP (primary) and WebSocket (debug/smoke tests).

## The idea

Not just a room/relay server. One process plays two roles:

- **Message relay** — RPC frames are routed between peers by target (single peer / master / broadcast).
- **Shared memory for the match** — the server holds the authoritative `RoomState` (peers, inputs, entities); each peer maintains a local replica fed by full/patch state frames.

Since authoritative state lives on the server and not on the host client, **host migration is seamless**: when the master leaves, another peer is elected and starts writing the `entities` section — no state transfer or ownership handoff is needed. The model is close to Photon Fusion *shared mode*: every client writes its own slice of shared state (inputs always, entities only when elected master), the server merges changes and replicates deltas.

- Room registry and peers, ticket admission with `nick_name`, authoritative `RoomState` (`StateRoomPolicy`).
- Per-layer details live in **package READMEs** (index below), not duplicated here.

## Quick start

```bash
# See .env.example — ADMISSION_SECRET is required (>= 32 bytes)
export ADMISSION_SECRET=replace-with-random-secret-at-least-32-bytes-long!!
export HTTP_API_KEY=replace-with-random-api-key-16-chars-min

go build -o bin/rooms ./cmd/rooms
go run ./cmd/rooms
```

- HTTP + WS: `http://localhost:3000`
- ENet (optional): UDP `7777` — `CGO_ENABLED=1 go build -tags enet ./cmd/rooms`

Devcontainer (VS Code / GitHub Codespaces): "Reopen in Container" — image with Go 1.25, gcc/pkg-config/libenet-dev for ENet, ports 3000 and 7777/udp already forwarded; see [.devcontainer/devcontainer.json](.devcontainer/devcontainer.json).

```bash
go test ./tests/...
# ENet cgo (gcc + CGO_ENABLED=1):
CGO_ENABLED=1 go test -tags enet ./tests/delivery/enet/...
```

Requirements: Go 1.25+; ENet needs CGO, `github.com/codecat/go-enet`, tag `enet`.

## Architecture

[docs/architecture.md](docs/architecture.md) — layers, dependencies, typical ticket → join → leave flow.

## Package documentation

When changing a package, update its `README.md` — [.cursor/rules/package-docs.mdc](.cursor/rules/package-docs.mdc).

| Area | Document |
|------|----------|
| **Client protocol (Godot)** | [docs/client-protocol/README.md](docs/client-protocol/README.md) → [enet.md](docs/client-protocol/enet.md) (primary), [llms.txt](docs/llms.txt) |
| Composition root | [cmd/rooms/README.md](cmd/rooms/README.md) |
| Domain | [internal/domain/README.md](internal/domain/README.md) |
| State model | [internal/domain/state/README.md](internal/domain/state/README.md) |
| Ports | [internal/port/README.md](internal/port/README.md) |
| Use cases | [internal/usecase/room/README.md](internal/usecase/room/README.md) |
| Config (env) | [internal/adapter/config/README.md](internal/adapter/config/README.md) |
| HTTP auth | [internal/adapter/httpauth/README.md](internal/adapter/httpauth/README.md) |
| HTTP limits | [internal/adapter/httplimits/README.md](internal/adapter/httplimits/README.md) |
| Admission (ticket) | [internal/adapter/admission/README.md](internal/adapter/admission/README.md) |
| Reservation | [internal/adapter/reservation/README.md](internal/adapter/reservation/README.md) |
| Metrics (prometheus) | [internal/adapter/metrics/README.md](internal/adapter/metrics/README.md) |
| Room registry | [internal/adapter/registry/README.md](internal/adapter/registry/README.md) |
| Realtime (Room, Peer) | [internal/adapter/realtime/README.md](internal/adapter/realtime/README.md) |
| StateRoomPolicy | [internal/adapter/realtime/policy/state/README.md](internal/adapter/realtime/policy/state/README.md) |
| Wire codec | [internal/adapter/realtime/codec/README.md](internal/adapter/realtime/codec/README.md) → [docs/wire-state-codec.md](docs/wire-state-codec.md) |
| Delivery (index) | [internal/delivery/README.md](internal/delivery/README.md) |
| HTTP API | [internal/delivery/http/README.md](internal/delivery/http/README.md) |
| Admin listener (`/metrics`) | [internal/delivery/admin/README.md](internal/delivery/admin/README.md) |
| WebSocket | [internal/delivery/ws/README.md](internal/delivery/ws/README.md) |
| ENet | [internal/delivery/enet/README.md](internal/delivery/enet/README.md) |
| Wire OpCodes | [internal/delivery/errors/README.md](internal/delivery/errors/README.md) |
| Tests | [tests/README.md](tests/README.md) |

## Project rules

- Go, Fiber, tests under `tests/`: [.cursor/rules/go-standards.mdc](.cursor/rules/go-standards.mdc)
- Configuration: [.cursor/rules/config.mdc](.cursor/rules/config.mdc)
- Package documentation: [.cursor/rules/package-docs.mdc](.cursor/rules/package-docs.mdc)
- Join / in-room OpCode checklist: [.cursor/rules/join-error-opcodes.mdc](.cursor/rules/join-error-opcodes.mdc)

## TODO

- Health-check
