# Channel ownership на shutdown: producer-каналы не закрываются

Ревью realtime-адаптеров нашло два `send on closed channel` panic. `Peer.Stop` закрывал `p.outbound` после `wg.Wait()`, но продюсеры `tryDeliver` (room fan-out, use cases) в `p.wg` не входят: TOCTOU между проверкой `ctx.Err()` и send в select. `EnetConnection.Close` закрывал `c.incoming`, в который `peerSession.deliver` шлёт из host-цикла ENet — та же гонка. Контракт `adapter/realtime` уже запрещал закрытие room-каналов; peer/enet ему противоречили. Решено: каналы, в которые пишут продюсеры вне WaitGroup потребителя, **никогда не закрываются** — `peer.outbound` и `EnetConnection.incoming` живут до GC. Consumers выходят по `ctx.Done()` или done-каналу: `EnetConnection` получил `closed chan struct{}` по образцу `WsConnection`, `Receive` — `select` на `closed`/`incoming`. Попутно `WsConnection.runPingLoop` при ошибке `WriteControl` вызывает `Close()` — иначе conn оставался `IsAlive() == true`: при `PeerIdleTimeout <= 0` sweep выключен и peer остаётся zombie навсегда, при `> 0` — выбивается по устаревшему `lastSeen` с задержкой до timeout.

## Considered Options

- **Включить продюсеров в WaitGroup потребителя** — отклонено: `tryDeliver`/`deliver` вызываются чужими горутинами (room loop, ENet host loop); их учёт в `wg` меняет ownership и блокирует `Stop` на внешних вызовах.
- **Atomic-флаг stopped перед send** — отклонено: не устраняет TOCTOU — между проверкой флага и send вклинивается `close`.
- **recover от panic в продюсерах** — отклонено: маскирует гонку и не решает ownership канала.

## Consequences

- Буфер мёртвого пира/ENet-сессии дренится GC вместе с объектом; продюсеры после `Stop` вместо panic получают ошибку/drop: `tryDeliver` — «peer stopped»/`domain.ErrQueueFull`, `peerSession.deliver` — `false` (host-цикл логирует drop).
- `EnetConnection.Receive` после `Close` может вернуть ещё буферизованные кадры до EOF — безвредно: consumer выходит по `closed`/ошибке Receive.
- `WsConnection`: неудачный `WriteControl` помечает conn закрытым — `IsAlive()` корректно уходит в `false`, zombie вычищается idle sweep'ом.
- Конвенция закреплена в go-standards: consumer завершается по ctx/done-каналу, а не по закрытию producer-канала.
