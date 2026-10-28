# Architecture

```text
cmd/rooms                 — composition root

delivery/                 — HTTP, WS, ENet, wire OpCode mappers
usecase/room/             — room scenarios
port/                     — interfaces (admission, httplimits, identity, logging, metrics, realtime, registry, reservation, transport)
adapter/                  — port implementations
domain/                   — types, events, sentinel errors (no I/O)
```

**Dependencies:** `delivery` → `usecase` → `port` ← `adapter`; `domain` depends on nothing.

## Planes

| Plane | Transport | Use cases |
|-------|-----------|-----------|
| Control | HTTP (`delivery/http`) | Create, Delete, GetList, IssueTicket, LeaveRoom |
| Data | WS (`delivery/ws`), ENet (`delivery/enet`) | JoinRoom, LeaveRoom, DisconnectPeer, ReconnectRoom |

## Typical flow

```text
IssueTicket (HTTP)  → join ticket with nick_name (+ hostID under ENet sharding, P18)
JoinRoom (WS/ENet)  → Validate → Admit → Peer → Room.Join/Replace → Start
LeaveRoom (HTTP)    → Room.Leave → Peer.Stop → Revoke          # explicit leave, no grace
DisconnectPeer      → MarkDisconnected → Room.Leave → Peer.Stop # transport drop: slot keeps
                                                                  # grace for reconnect (P20);
                                                                  # sweep revokes on expiry
```

## Conventions

- HTTP — Fiber v3 only; a single `*fiber.App` serves REST and the WS upgrade.
- Handlers in `delivery` are thin: parse → use case → response/error.
- Tests live only under `tests/` (see [tests/README.md](../tests/README.md)).
- Agent rules: [.cursor/rules/go-standards.mdc](../.cursor/rules/go-standards.mdc), [.cursor/rules/package-docs.mdc](../.cursor/rules/package-docs.mdc).
