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

**ENet host / HostID** (shard):
Один ENet host процесса: свой UDP-порт из `EnetConfig.ListenPorts` и свой `HostLoop`. `domain.HostID` - индекс+1 в списке портов; попадает в claims ticket'а для strict binding admit (P18). `HostIDUnbound` (`0`) - ticket без привязки (выписан без HostRegistry, WS-only) → reject `OpWrongHost` на ENet-admit. Комната транспорт-агностична: её peer'ы могут сидеть на разных host'ах.
_Avoid_: shard index, node, listener id

**Orphan admitted**:
Слот reservation в состоянии `admitted`, для которого в комнате нет живого peer'а (клиент получил ticket и зашёл, но peer ушёл/умер без revoke). Вычищается фоновым `SweepOrphanAdmittedUseCase` после `lastSeen + RESERVATION_RECONNECT_TTL` (P20) или точечным revoke+reserve в `IssueTicket` при повторном issue того же peer'а.
_Avoid_: stale slot, dangling slot, orphan reservation

**Grace window** (окно реконнекта):
Период после разрыва транспорта, в течение которого peerID можно воскресить reconnect-токеном. Peer при этом удаляется из комнаты сразу (delete-peer модель, P20): grace живёт на admitted-слоте reservation — слот держит capacity и отсчитывает `disconnectedAt + RESERVATION_RECONNECT_TTL`; по истечении sweep делает Revoke.
_Avoid_: reconnect TTL от issue тикета, suspended peer, offline peer

**Reconnect token**:
HMAC-credential (kind `2`) для resume под тем же peerID: `kind | roomID | peerID | hostID | MAC` — без nickName и expiry, grace — серверное состояние на слоте. Отличен от join-тикета (kind `1`) по kind-байту credential'а и `Claims.Kind` в claims; одноразовый входной тикет и reconnect-credential — разные сущности.
_Avoid_: resume ticket (это термин про peerID в join-ticket'е), session token

**Ghost input**:
Входной кадр (input/RPC/state) от peer'а, которого уже нет в комнате (zombie или удалённый без вычистки из state). Policy отклоняет такие кадры проверкой членства (`ErrPeerNotFound`), иначе state получал бы записи от несуществующего peer'а.
_Avoid_: phantom input, stale input

**Masterless episode** (эпизод без мастера):
Период, когда в комнате есть живые peers, но `p.master == nil` — обычно после упавшего election. Переизбрание идёт на patch-тиках и лениво при RPC→master; `Error` об эпизоде логируется один раз (не на каждый тик), `Info` — при восстановлении.
_Avoid_: election gap, missing master

**Admin listener**:
Отдельный Fiber-инстанс вне публичного API (`ADMIN_LISTEN_ADDR`, пустой дефолт = выключен): служебные endpoint'ы процесса, сейчас `GET /metrics` (prometheus exposition, P19). Не путать с REST control plane на `HTTP_ADDR` — там auth + rate limits, тут голая телеметрия для scrape.
_Avoid_: metrics port, health server, internal API

**Authoritative host** (room policy `authoritative-host`, P29):
Политика комнаты при которой master-peer — единственный «хост» матча: его уход (leave И disconnect, grace не применяется) убивает комнату целиком — оставшимся `OpRoomClosed` (`0x82`) + disconnect, слоты ревокаются. Антоним — дефолтный `state` с master-миграцией.
_Avoid_: host-pinned, dedicated host, listen server flag

**Host migration** (master migration):
Перевыборы master'а при его уходе (`electAndAssign`, min ping → min PeerID) — есть только при полиси `state`; в `authoritative-host` миграции нет, комната умирает.
_Avoid_: failover host, master handoff

**Master election semantics**:
Правила выбора master в `StateRoomPolicy`: stale-кандидаты (`ErrPeerNotFound` от `GetPeer` — peer в state, но не в room) пропускаются, выборы идут среди живых; все stale → пустые выборы (`PeerIDInvalid`, не ошибка); прочие ошибки `GetPeer` фейлят выборы; `IsMaster`-флаги пишутся в state только после успешной проверки избранного peer (validate-first) — state не рекламирует master, которого нет у policy.
_Avoid_: quorum, leader election

**Peer drain** (drain-aware close, P30):
Слив `peer.outbound` при `Peer.Stop` до закрытия соединения: по отмене ctx write-loop дочитывает закоммиченные кадры и вызывает `Send`, `Stop` ждёт слива в пределах `TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT` (`0` — выключен) и только затем `conn.Close`. Гарантия — «`Send` до `Close`», не «клиент получил». Не путать с **grace window**: grace — уровень reservation (воскрешение peerID после разрыва), drain — транспортный flush последних кадров на прощание.
_Avoid_: graceful disconnect (это про ENet `DisconnectLater`), send flush, linger

**Empty-room death** (смерть пустой комнаты, P31):
Уничтожение комнаты, в которой не осталось ни живых peers, ни admitted-слотов reservation. Два подкласса: **emptied** — ушёл последний peer (transition-триггер `closeRoomIfEmpty` в `LeaveRoom`/`DisconnectPeer`/`EvictIdlePeers`/orphan-sweep revoke); **never-joined** — комната создана, но никто не вошёл (time-based `SweepEmptyRoomsUseCase`: kill когда все reserve истекли или слотов нет). Broadcast `0x82` не шлётся — слушать некому. Отличие от **authoritative-host death** (P29): та убивает комнату с живыми peers по уходу хоста и рассылает `0x82` оставшимся; общий teardown (`DeleteRoom` → `UnregisterRoom` → Stop), разные триггеры.
_Avoid_: room GC, idle room cleanup, room reaper
