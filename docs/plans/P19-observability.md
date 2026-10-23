# P19 — Observability

## Контекст

Единственный канал наблюдаемости — zap-логи. Нет метрик, нет трейсинга, нет
способа ответить «сколько комнат живо, сколько peer'ов, какой rate
join/reject, насколько переполнены очереди». Для продакшена это слепая зона:
рассинхрон и деградации обнаруживаются постфактум.

## Цель

Экспортировать из процесса сигналы, достаточные для: (а) дашборда
«здоровье системы», (б) отладки рассинхрона/перегруза, (в) алертов.
Без тяжёлой инфраструктурной зависимости в ядре.

## Решения (зафиксированы гриллингом)

- **Backend**: Prometheus (`client_golang`), pull-модель.
- **Инструментация**: `port.Metrics` — потребительский интерфейс
  (Counter/Gauge/Histogram фабрики), DI через конструкторы usecase'ов и
  realtime-слоёв; `adapter/metrics` — реализация поверх prometheus
  registry. Мокабельно в тестах, согласуется с port-стилем.
- **Endpoint**: отдельный admin listener (второй Fiber-инстанс),
  `ADMIN_LISTEN_ADDR` (напр. `:9091`), `GET /metrics` — телеметрия
  изолирована от публичного API. Env-флаг включения.
- **Скоуп сигналов**: таблица ниже — минимум для дашборда + отладки
  рассинхрона; без per-peer label'ов (кардинальность).

## Скоуп правок

- `internal/port/metrics/` — интерфейс.
- `internal/adapter/metrics/` — prometheus-реализация + config
  (`MetricsConfig`, `METRICS_*` env, `Validate`).
- `internal/adapter/config` — поле + loader.
- `internal/delivery/admin/` — listener `/metrics` (promhttp-адаптер на Fiber).
- usecase/room + realtime + delivery/enet — точечные вызовы метрик.
- `cmd/rooms` — wire.
- docs: README пакетов, ADR.

## Кандидаты на сигналы (черновик)

| Слой | Сигнал |
|------|--------|
| HTTP | req count/duration по route, 4xx/5xx |
| Комнаты | active rooms, peers per room, joins/leaves/rejects (по opcode), master elections |
| Room loop | incoming queue depth, drops (ErrQueueFull), tick duration |
| Peer | outbound queue depth, drops, send errors |
| ENet | hosts, peers per host (уже считается в enetHost.peers), admits/wrong-host rejects, HostLoop queue latency |
| Reservation | active slots, reserved→admitted, orphan revokes |
| Sweeps | runs, evicted per sweep |

## Верификация

- юнит-тесты реестра метрик; e2e smoke `GET /metrics` в env
- lint/vet/build; tagged прогон если трогаем enet-файлы

## Реализация (итог)

- **port/metrics** (`internal/port/metrics/metrics.go`): плоский
  интерфейс `Metrics` из 14 типизированных методов в стиле
  `port/logging.Logger` - доменные глаголы (`PeerJoined`,
  `JoinRejected(op)`, `RoomIncomingDropped`, `MasterElected`,
  `ReservationReserved/Admitted/Revoked`, `OrphanAdmittedRevoked(n)`,
  `IdlePeersEvicted(n)`, gauges `SetActiveRooms`/`AddActivePeers`).
  Маппинг на имена/label'ы метрик - внутри адаптера; кардинальность
  ограничена типами параметров (reject по `domain.OpCode`). `Noop()` и
  `OrNoop(m)` - nil-нормализация DI.
- **adapter/metrics**: `Metrics` поверх собственного
  `prometheus.Registry` (не default), namespace `rooms_*`; 8 семейств +
  стандартные `go_*`/`process_*` коллекторы. `join_rejects_total{opcode}`
  (hex), `queue_drops_total{queue=room_incoming|peer_outbound|enet_incoming}`,
  `reservation_ops_total{op}`, `sweep_processed_total{sweep}` -
  все label'ы фиксированным множеством. `Gatherer()` для exposition.
  `MetricsConfig.AdminListenAddr` + `Validate()` (`net.ResolveTCPAddr`).
- **adapter/config**: `Config.Metrics`, env `ADMIN_LISTEN_ADDR`;
  **пустой дефолт = admin listener выключен** (scrape opt-in).
- **delivery/admin**: `NewMetricsApp(gatherer)` - отдельный
  `*fiber.App`, `GET /metrics` ручным `Gather()` + `expfmt` энкодером
  (без `net/http`/promhttp-adaptor - правило "только Fiber").
- **Эмит по слоям**: `Room` - joins/leaves/`active_peers` на
  lifecycle-переходах (`Replace` = пара leave+join), drop
  `room.incoming`; `Peer` - drop `outbound`; `RoomRegistry` -
  `active_rooms` по `len(rooms)`; `Reservation` - переходы слотов;
  `masterMachine.assign` - `master_elections_total`; `delivery/ws` и
  `delivery/enet` (admit-путь) - `join_rejects_total{opcode}`; drop
  очереди ENet-сессии - `queue_drops{enet_incoming}`; sweep use-case'ы
  - processed за проход.
- **DI**: конструкторы фабрик/registry/reservation/policy-factory/
  handler'ов/sweep use-case'ов принимают `Metrics` (nil-safe через
  `OrNoop`); `NewRoom`/`NewPeer`/`NewStateRoomPolicy` сигнатуры не
  тронуты - фабрики вливают metrics полем (умолчание `Noop` в
  конструкторе), поэтому ~30 white-box тестовых вызовов не менялись.
- **cmd/rooms**: `metricsadapter.New` до wiring; второй Fiber-инстанс
  на `ADMIN_LISTEN_ADDR`; его listen error - fail-fast как у основного;
  `ShutdownWithContext` при остановке.
- **Mocks**: `tests/mocks/mock_metrics.go` (go generate).
- **Тесты**: `tests/adapter/metrics` (счётчики через `Gather`,
  `MetricsConfig.Validate`), `tests/delivery/admin` (`GET /metrics` -
  200 + exposition), `tests/adapter/config` (`ADMIN_LISTEN_ADDR`
  default/custom/invalid).
- **Зависимость**: `client_golang v1.24.1` (не v1.25.0 - та тянет
  `go 1.26` и ломала бы enet-образ на Go 1.25.x).

### Отклонения/нюансы

- Peers gauge - суммарный (`AddActivePeers` дельтами на lifecycle),
  не per-room: label по roomID давал бы churn серий.
- Reject-счётчик - на границе отправки кадра (ws handler + enet admit),
  а не в `SendJoinReject`: порт в package-функцию delivery/errors не
  тянется без глобали; покрывает оба транспорта, включая `OpWrongHost`.
- `run_cgo.go` тронут минимально (2 строки эмита); verified
  `./scripts/enet-verify.sh build`.
- HTTP req count/duration и HostLoop queue latency из черновой таблицы
  не реализованы - зафиксированный минимум их не требовал.
- **Верификация**: `go build ./...`, `go vet ./...`, `golangci-lint run`
  (0 issues), `go generate ./tests/mocks`, `go test -race -short` зелёный,
  `./scripts/enet-verify.sh build` зелёный.

Исправления и дополнение сигналов по итогам code-review -
[docs/plans/P19b-review-remediation.md](P19b-review-remediation.md).
