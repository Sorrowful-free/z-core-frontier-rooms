# Observability: prometheus через плоский `port.Metrics`, отдельный admin listener, opt-in scrape

До P19 единственным каналом наблюдаемости были zap-логи: ни дашборда, ни алертов, ни способа ответить «сколько комнат живо и насколько переполнены очереди». Решено: prometheus (`client_golang`), pull-модель, собственный `Registry` (не default) в `adapter/metrics`, экспорт по `GET /metrics`.

## Decision

- **Потребительский интерфейс `port/metrics.Metrics` - плоские доменные методы**, а не фабрики `Counter/Gauge/Histogram`. Исходная формулировка гриллинга предлагала фабричный интерфейс; отклонено осознанно: доменные глаголы (`PeerJoined`, `JoinRejected(op)`, `MasterElected`, `RoomTickObserved(kind, d)`) мокабельнее в тестах и жёстко ограничивают кардинальность label'ов типами параметров (имена метрик и множества label'ов - приватная деталь адаптера, эмиттеры не могут их раздуть). `Noop()`/`OrNoop(m)` - nil-нормализация DI, конструкторы принимают nil.
- **DI полем через фабрики** (`RoomFactory`, `PeerFactory`, `StateRoomPolicyFactory`): сигнатуры `NewRoom`/`NewPeer`/`NewStateRoomPolicy` стабильны для white-box тестов, умолчание `Noop`.
- **Отдельный admin listener** (второй `*fiber.App`, `delivery/admin`): телеметрия изолирована от публичного API и не проходит через его auth/rate-limit middleware. `GET /metrics` - ручной `Gatherer.Gather()` + `expfmt` энкодер, без `net/http`/promhttp-adaptor (правило «только Fiber»).
- **Opt-in по пустому адресу**: `ADMIN_LISTEN_ADDR` пуст по умолчанию - scrape-endpoint не биндится без явного env; коллекция сигналов при этом работает всегда (затраты мизерные).
- **Bind admin listener'а - пред-созданный `net.Listener`** (P19b): bind идёт синхронно в `run()` до serve-горутины - bind failure виден сразу, race «Shutdown до Listen» исключён, shutdown admin получает свой timeout-ctx.
- **Счётчики привязаны к реальным событиям, не попыткам** (P19b): `join_rejects_total` - только успешно отправленные reject-кадры (`SendJoinReject` возвращает ошибку send); `active_peers` и `peer_joins`/`peer_leaves` - по факту мутации `peers` map на room-loop'е; `reservation_ops{revoke}` - любое реальное удаление слота; `master_elections_total` - первый элекшн и перевыборы.

## Considered Options

- **Фабричный интерфейс `Counter/Gauge/Histogram` в port** - отклонено: синглтоны метрик пришлось бы создавать где-то на стороне потребителя (per-room/per-peer фабрики - churn серий), а произвольные имена/label'ы снимают контроль кардинальности, который и есть главная ценность плоского интерфейса.
- **promhttp / `net/http` handler для exposition** - отклонено: правило «только Fiber»; ручной `expfmt` даёт тот же текст без второго стека.
- **Endpoint на публичном app** - отклонено: `/metrics` получил бы auth/limits проходки и смешался бы с control plane; отдельный listener дешёв и позволяет прибить scrape к loopback при желании.
- **Per-room/per-peer label'ы** - отклонено: неограниченная кардинальность; агрегированные gauge'ы (`active_peers`, `enet_host_peers` с host-label, ограниченным числом портов) достаточны для дашборда здоровья.
- **Queue depth как точная сумма** - отклонено в пользу сэмплов `len(chan)` на enqueue/dequeue: приближение last-write-wins документировано, альтернатива (дельта-учёт) дрейфует на незакрываемых каналах и усложняет ownership.

## Consequences

- `port.Metrics` вырос до 23 методов (P19 + P19b сигналы); моки перегенерируются `go generate ./tests/mocks/...`.
- Эмиты, стоящие внутри stage-then-commit транзакций policy (`MasterElected` в `assign`/`onPeerJoined`), считают staged-события: откат транзакции оставляет редкий пересчёт - принятое приближение, точный commit-only учёт потребовал бы отложенной очереди событий.
- `NewHostLoop` получил параметр `Metrics` (queue latency summary, агрегировано по host'ам - без host-label); tagged callsites обновлены, проверка - `scripts/enet-verify.sh`.
- `SendJoinReject`/`SendReservationReject`/`SendInRoomError` возвращают `error` - обратно-совместимо для caller'ов, игнорирующих результат.
- Тройной listener-паттерн в `main.go` (HTTP + admin + ENet select-loop) остаётся как есть: симметричный перевод публичного `fiberApp` на pre-bound `net.Listener` отложен (не входил в скоуп P19b).
