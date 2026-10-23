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

## Риски

- libenet callbacks/peers не потокобезопасны — любой пропущенный вызов вне
  loop'а = data race (проверять -race прогоном e2e)
- deadlock loop'а на shutdown (Stop должен дождаться drain)

## Верификация

- `./scripts/enet-verify.sh` полный: tagged build/vet/tests + все 3 e2e
  под -race (mixed-сценарий — лучший стресс конкурентных send/service)
- Замер до/после: латентность e2e-дропа, длительность e2e-прогона
