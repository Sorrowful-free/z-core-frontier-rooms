# ENet host sharding по peer'ам с server-side балансировкой ticket'ов

Один ENet host гоняет все пакеты всех peer'ов через один `HostLoop` (`host.Service`): потолок по pps/latency и `PeerLimit <= 4095` на host. После P15 (снятие глобального apiMu) host'ы независимы, а room - транспорт-агностик (mixed e2e уже доказал, что peer'ы разных транспортов живут в одной комнате). Решено: N UDP listener'ов (`ENET_LISTEN_PORTS` CSV, один `HostLoop` на порт), shard'ирование по **peer'у** - соседи по комнате могут сидеть на разных host'ах.

Discovery - через ответ issue ticket: сервер выбирает host (`HostRegistry.AssignHost`: least-loaded по живым peer'ам host'а, round-robin tie-break) и возвращает `enet_port` в JSON; клиент подключается на назначенный порт. Привязка строгая: ticket v3 несёт `host_id` в claims; admit на чужом host'е - join-reject `OpWrongHost` (`0x77`, band 0x70-0x7F) до `JoinRoom`, слот reservation не тратится. Claims без `host_id` (ticket v2, `HostIDAny`) admit'ятся на любом host'е - backward compat и путь для WS-тикетов.

`HostID` - доменный тип (индекс+1 в `ListenPorts`), не порт: ticket несёт identity host'а, а не транспортную деталь. `PeerLimit` остаётся per-host; capacity комнаты к host'у не привязана. `RoomsHandler` - mux над N run-горутинами с общим ctx: `Stop` fan-out, `Done` закрывается после выхода всех loop'ов, первый упавший run - fail-fast (`Err` = первая ошибка).

## Considered Options

- **Shard по комнате** - отклонено: размер комнаты не коррелирует с её пакетной нагрузкой; peer-granularity размазывает нагрузку ровнее и не требует выбора host'а при create.
- **Client-side host selection / DNS** - отклонено: у клиента нет данных о загрузке; сервер знает живые счётчики и сам распределяет в момент issue.
- **Ticket без привязки + redirect** - отклонено: redirect в ENet нет, а лишний round-trip усложняет admit; hostID в claims - дешёвая строгая проверка до трогания reservation.
- **Least-loaded по admitted peers** - отклонено в пользу живых ENet-сессий (`peers` на `EventConnect`): ticket issue'ется до admit'а, счётчик по admitted запаздывал бы и переоценивал бы пустые host'ы.

## Consequences

- Формат ticket вырос на 2 байта (v3); `Verify` принимает v2 и v3 - старые тикеты валидны.
- HTTP contract: `enet_port` в ответах create и `/tickets` (`omitempty` - без ENet listener'а поля нет, WS-клиенты его игнорируют).
- Double `admission.Validate` на admit: сначала в delivery для hostID-проверки, затем внутри `JoinRoom` - цена за reject до `reservation.Admit`.
- Конфиг: `ENET_LISTEN_PORTS` CSV; `ENET_LISTEN_PORT` - alias, побеждает если задан (single-port деплой не меняется).
- Балансировка в usecase (`port/transport/enet.HostRegistry`), delivery только валидирует binding: слой transport-сведений в usecase - осознанная утечка через порт, альтернатива (hostID через параметр JoinRoom) смешивала бы assignment с admission.
