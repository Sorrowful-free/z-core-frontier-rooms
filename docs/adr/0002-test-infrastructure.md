# Тестовая инфраструктура: общий env, реальный WS-listener, скоуп integration

Тестовый бэклог (`tests/README.md` → «Следующий приоритет») требует WS-тестов и первого сквозного сценария. Решено: единый конструктор composition выносится в `tests/testutil/env` (wiring registry + reservation + admission + use cases), существующие `tests/delivery/http` мигрируют на него; WS-тесты идут через реальный `net.Listen("127.0.0.1:0")` + `app.Listener` + `websocket.DefaultDialer` из `github.com/fasthttp/websocket`; первый `tests/integration/` сценарий — `create (host)` → `issue (joiner)` → оба join по WS → 2 peers → joiner disconnect → 1 peer.

## Considered Options

- **`app.Test` для WS** — отклонено: Fiber `app.Test` не делает настоящий WebSocket upgrade, путь token→opcode не покрыть.
- **Дублировать wiring в каждом пакете** — отклонено: три копии composition (http/ws/integration) при любом изменении конструкторов use case'ов.
- **Integration только с joiner'ом** — отклонено: без host'а по WS не проверяется ни реальный `LeaveRoom` через transport, ни мульти-peer состояние комнаты.

## Consequences

- `github.com/fasthttp/websocket` становится прямой зависимостью `go.mod` (используется в `tests/` и production WS-транспорте).
- Изменение конструктора любого use case правит один env, а не N тестовых файлов.
- Порядок закрытия бэклога: unit-пачка (Join `errors.Join`, `Send` ExcludePeerID, `Peer.Stop`) → ws → integration; `go test -race ./tests/...` после каждой пачки.
