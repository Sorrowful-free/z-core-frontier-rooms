# z-core-frontier-rooms

Сервис игровых комнат: control plane (HTTP) и data plane (WebSocket, ENet). Термины домена комнат, peers и живости соединений.

## Language

**Zombie peer**:
Peer, который остаётся зарегистрированным в комнате, но его транспорт фактически мёртв (полуоткрытый TCP и т.п.). Слот reservation занят, кадры клиенту не доходят.
_Avoid_: dead peer, ghost peer, stale peer

**Idle eviction**:
Периодический sweep, выводящий zombie peer'ов из комнат через стандартный leave-поток (`Leave` → `Stop` → `Revoke`).
_Avoid_: reaper, cleaner, GC peers

**IsAlive**:
Контракт живости соединения/peer'а: `false` — транспорт считается мёртвым и peer подлежит idle eviction. Каждая реализация транспорта сама решает, что считается живостью.
_Avoid_: Connected, Active, Online

**Last seen**:
Внутренний момент последней подтверждённой живости клиента. Для WS — успешный входящий кадр или pong; исходящие кадры живость не подтверждают. Не путать с RTT (`Ping()`): RTT — устаревающее значение, а не метка времени.
_Avoid_: LastActivity, heartbeat timestamp
