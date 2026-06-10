# Архитектура

```text
cmd/rooms                 — composition root

delivery/                 — HTTP, WS, ENet, wire OpCode mappers
usecase/room/             — сценарии комнаты
port/                     — интерфейсы (admission, registry, reservation, realtime, transport)
adapter/                  — реализации port
domain/                   — типы, события, sentinel-ошибки (без I/O)
```

**Зависимости:** `delivery` → `usecase` → `port` ← `adapter`; `domain` ни от кого не зависит.

## Плоскости

| Плоскость | Транспорт | Use case |
|-----------|-----------|----------|
| Control | HTTP (`delivery/http`) | Create, Delete, GetList, IssueTicket |
| Data | WS (`delivery/ws`), ENet (`delivery/enet`) | JoinRoom, LeaveRoom |

## Типичный поток

```text
IssueTicket (HTTP) → ticket v2 с nick_name
JoinRoom (WS/ENet)  → Validate → Admit → Peer → Room.Join/Replace → Start
LeaveRoom           → Room.Leave → Peer.Stop → Revoke
```

## Соглашения

- HTTP — только Fiber v3; один `*fiber.App` для REST и WS upgrade.
- Handlers в `delivery` тонкие: парсинг → use case → ответ/ошибка.
- Тесты — только `tests/` (см. [tests/README.md](../tests/README.md)).
- Правила агентов: [.cursor/rules/go-standards.mdc](../.cursor/rules/go-standards.mdc), [.cursor/rules/package-docs.mdc](../.cursor/rules/package-docs.mdc).
