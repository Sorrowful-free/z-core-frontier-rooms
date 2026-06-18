# adapter/logging/zap

Адаптер `port/logging.Logger` поверх `go.uber.org/zap` + конфиг корневого логгера.

## RootConfig

- `LOG_MODE=prod` (default) → `zap.NewProduction()`
- `LOG_MODE=dev` → `zap.NewDevelopment()`

`NewFrom(*zap.Logger, tag)` — именованные дочерние логгеры для модулей.

Env-индекс: [adapter/config/README.md](../../config/README.md).
