# P15: dispatch-to-host-loop вместо глобального apiMu

## Проблема

`apiMu` — глобальный мьютекс, сериализующий ВСЕ вызовы libenet
(`adapterenet.Call/Do`): серверный `host.Service` (блокирующий, ServiceTimeoutMs),
send из N соединений, receive-диспатч, тестовые client-host'ы. Следствия:

- `Service(timeout)` держит глобальный лок — в худшем случае send-кадр ждёт
  полный service-таймаут
- в тестах client-host'ы душат друг друга (агенту P12 пришлось опустить
  Service(10) → Service(1) из-за конвоя)
- масштабируемость: один латентный путь стопорит всех

## Цель

Per-host сериализация вместо глобальной: у каждого `libenet.Host` — loop-
goroutine с очередью команд (Service-tick, send, connect, disconnect, destroy).
libenet не thread-safe → все вызовы хоста идут через его loop; глобальный лок
остаётся только для init/deinit libenet-библиотеки (если требуется).

Серверный host уже имеет событийный loop (`run_cgo.go` Service → dispatch) —
расширить его до полноценного command-loop; клиентские хосты в тестах получают
тот же механизм (или остаются на apiMu — отдельное решение).

## Границы

- Универсальный host-loop для ЛЮБОГО host'а, включая тестовые клиентские
  (решение grill Q2, вариант b): `Call/Do` перерабатываются в host-scoped
  диспатч — вызов адресуется конкретному host'у и сериализуется его
  очередью, а не глобальным apiMu
- Только tagged-код: `internal/adapter/transport/enet/*_cgo.go`,
  `internal/delivery/enet/*_cgo.go`, тестовые enet-клиенты
- Не менять wire-поведение, admission, каналы/флаги пакетов

## Решения (grill-with-docs round 2)

- **Q1 - API**: `HostLoop` объект (host + goroutine + очередь), но для
  connection-пути он вшит **внутрь адаптера**: `EnetConnection` хранит
  `*HostLoop` с момента создания, `Send/Ping/Close` шлют команды в его
  очередь - потребители `transport.Connection` libenet не видят.
  Наружу API торчит только для владельцев host'а (server `Listen`,
  тестовые клиенты). `run_cgo.go` Service-loop - тело host-loop'а;
  `session_cgo`/`frame_cgo` работают внутри Service-диспатча, им диспатч
  не нужен вовсе.
- **Q2 - Service**: `Service(ServiceTimeoutMs)` как сейчас + drain очереди
  после каждого возврата. Send-latency сервера не меняется (~10ms worst) -
  выигрыш: per-host изоляция (уходит тестовый конвой P12) + чистый
  ownership. `Service(1)` - отложенная опция, если latency станет важна.
- **Q3 - Initialize/Deinitialize**: без лока - вызов до старта любых
  host-loop'ов, гонки нет по построению.
- **Q4 - привязка**: `Peer.GetHost()` нет - `*HostLoop` передаётся явно:
  `NewEnetConnectionFactory(loop, logger)` (loop в конструкторе фабрики),
  клиент получает loop после `Connect`.
- **Q5 - shutdown**: `loop.Stop()` перестаёт принимать команды → drain →
  `host.Destroy` → ждущие Caller'ы получают ошибку; `ListenHandle.Stop()`
  идёт через `loop.Stop()`. Клиентский host-loop - свой Stop/Destroy.

## Риски

- libenet callbacks/peers не потокобезопасны — любой пропущенный вызов вне
  loop'а = data race (проверять -race прогоном e2e)
- deadlock loop'а на shutdown (Stop должен дождаться drain)

## Верификация

- `./scripts/enet-verify.sh` полный: tagged build/vet/tests + все 3 e2e
  под -race (mixed-сценарий — лучший стресс конкурентных send/service)
- Замер до/после: латентность e2e-дропа, длительность e2e-прогона

## Итог реализации

- `HostLoop` (`adapter/transport/enet/hostloop_cgo.go`): владеет host'ом,
  loop-горутина - FIFO-executor команд. `Call[T](loop, fn)`/`loop.Do(fn)` -
  blocking submit для владельцев (server-драйвер service-итераций, клиентский
  pump гоняет `Service` командами, `EnetConnection.Ping`); `loop.enqueue` -
  async для `Send`/`Close`. `Stop`: закрытие приёма blocking-команд → drain
  (все принятые исполняются) → `host.Destroy`; submit'ы после stop -
  `ErrHostLoopStopped`, ожидающие caller'ы разблокируются.
- Server `run`: `Service` - blocking-команда в loop, dispatch на run-горутине
  (peer/packet-операции - Call/Do в тот же loop). `EnetConnection` хранит
  `*HostLoop`; фабрика `NewEnetConnectionFactory(loop, logger)` + `BindLoop`
  (loop рождается позже конструктора фабрики).
- Отступление от design-note «обёртки в session/frame сняты»: dispatch
  НЕ исполняется на loop-горутине - admit/leave-путь (`JoinRoom` →
  `dispatchLifecycle`) синхронно ждёт room-горутину, а та зовёт
  `conn.Ping` → blocking Call в тот же loop: на executor'е это был бы
  cross-deadlock executor↔room (пойман e2e). Executor поэтому исполняет
  только ограниченные по времени libenet-вызовы, а обёртки остались -
  как host-scoped Call/Do (Q2 плана буквально).
- Отступления от списка файлов: `cmd/rooms/main.go`,
  `tests/testutil/env/env.go`, `internal/delivery/enet/room_handler.go`,
  `connection_factory_stub.go` + тесты фабрики - новая сигнатура
  `NewEnetConnectionFactory(nil, logger)` требует правок во всех build-ветках;
  `hostloop_stub.go` даёт untagged-тип для сигнатур; README пакетов обновлены
  по факту (package-docs rule).
