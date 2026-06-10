# delivery

Транспорт и маппинг на wire. Зависит от `usecase`, не от `adapter` напрямую (кроме composition в `cmd`).

| Пакет | README |
|-------|--------|
| `http/` | [http/README.md](http/README.md) — REST control plane |
| `ws/` | [ws/README.md](ws/README.md) — WebSocket data plane |
| `enet/` | [enet/README.md](enet/README.md) — ENet UDP |
| `errors/` | [errors/README.md](errors/README.md) — OpCode join / in-room / reservation |

Handlers тонкие: парсинг → use case → ответ или wire-ошибка.
