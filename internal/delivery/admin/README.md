# delivery/admin

Admin listener процесса (P19): отдельный `*fiber.App` вне публичного API — телеметрия не смешивается с control/data plane и не проходит через их auth/rate-limit middleware.

## Endpoint

`GET /metrics` — prometheus exposition (`text/plain; version=0.0.4`): сигналы приложения (`rooms_*`, см. [adapter/metrics](../../adapter/metrics/README.md)) + стандартные `go_*`/`process_*` коллекторы.

Реализация — ручной `Gatherer.Gather()` + `expfmt` энкодер (без `net/http`/promhttp-adaptor: правило «только Fiber»). `NewMetricsApp(gatherer, logger)`: ошибки gather/encode логируются и отдаются как 500 с обёрнутым контекстом (P19b), не голый `ErrInternalServerError`.

## Listen

Адрес — `ADMIN_LISTEN_ADDR` (`adapter/metrics.MetricsConfig.AdminListenAddr`); **пустой дефолт = listener выключен**, endpoint поднимается только явным env. `cmd/rooms` биндит `net.Listener` синхронно до serve-горутины (P19b: bind failure виден сразу, нет race «shutdown до listen») и гасит app своим timeout-ctx (`ShutdownWithContext`).
