# port/metrics

Потребительский порт телеметрии процесса (P19). Реализация — [`adapter/metrics`](../../adapter/metrics/README.md) (prometheus), экспорт — `GET /metrics` на отдельном admin listener'е ([`delivery/admin`](../../delivery/admin/README.md)).

## Интерфейс

`Metrics` — плоский интерфейс типизированных сигналов (в стиле `port/logging.Logger`): компоненты сообщают доменные события, маппинг на имена/label'ы метрик — внутри адаптера. Кардинальность ограничена типами параметров (reject counter по `domain.OpCode`).

| Группа | Методы |
|--------|--------|
| Gauges | `SetActiveRooms`, `AddActivePeers`, `RoomIncomingDepth(n)`, `PeerOutboundDepth(n)`, `EnetHostPeers(hostID, n)`, `ReservationActiveSlots(delta)` |
| Комнаты | `PeerJoined`, `PeerLeft`, `JoinRejected(op)`, `MasterElected`, `RoomTickObserved(kind, d)` |
| Очереди | `RoomIncomingDropped`, `PeerOutboundDropped`, `EnetIncomingDropped`, `EnetHostLoopQueueLatency(d)` |
| Peer | `PeerSendError`, `PeerDisconnected`, `ReconnectSucceeded` |
| Reservation | `ReservationReserved`, `ReservationAdmitted`, `ReservationRevoked` |
| Sweeps | `OrphanAdmittedRevoked(n)`, `IdlePeersEvicted(n)`, `SweepRan(sweep)` |
| HTTP | `HTTPRequestObserved(route, method, status, d)` |

`PeerJoined`/`PeerLeft` считают переходы членства на room-loop'е: `Replace` = leave + join (пара событий, gauge peer'ов без дельты). Метрики членства привязаны к реальной мутации `peers` map (P19b): повторный join того же peer'а и повторный leave не искажают `active_peers`.

Gauge-сигналы глубины очередей - сэмплы `len(chan)` в точках enqueue/dequeue (приближение last-write-wins между инстансами, не строгая сумма). `JoinRejected` считается только после успешной отправки reject-кадра: `delivery/errors.SendJoinReject` возвращает ошибку send, caller эмитит по её результату.

## Noop

`Noop()` / `OrNoop(m)` — заглушка и nil-нормализация: компоненты принимают `nil` в конструкторах и нормализуют к `Noop`, поэтому прямое конструирование в тестах безопасно без мока.
