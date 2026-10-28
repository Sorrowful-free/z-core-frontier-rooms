# delivery/admin

Process admin listener (P19): a separate `*fiber.App` outside the public API — telemetry is not mixed with the control/data plane and does not go through their auth/rate-limit middleware.

## Endpoint

`GET /metrics` — prometheus exposition (`text/plain; version=0.0.4`): application signals (`rooms_*`, see [adapter/metrics](../../adapter/metrics/README.md)) + standard `go_*`/`process_*` collectors.

Implementation — manual `Gatherer.Gather()` + `expfmt` encoder (no `net/http`/promhttp adaptor: "Fiber only" rule). `NewMetricsApp(gatherer, logger)`: gather/encode errors are logged and returned as 500 with wrapped context (P19b), not a bare `ErrInternalServerError`.

## Listen

Address — `ADMIN_LISTEN_ADDR` (`adapter/metrics.MetricsConfig.AdminListenAddr`); **empty default = listener disabled**, the endpoint comes up only via explicit env. `cmd/rooms` binds `net.Listener` synchronously before the serve goroutine (P19b: bind failure is visible immediately, no "shutdown before listen" race) and shuts the app down with its own timeout ctx (`ShutdownWithContext`).
