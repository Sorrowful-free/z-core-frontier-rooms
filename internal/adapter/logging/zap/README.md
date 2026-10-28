# adapter/logging/zap

Adapter of `port/logging.Logger` on top of `go.uber.org/zap` + root logger config.

## RootConfig

- `LOG_MODE=prod` (default) → `zap.NewProduction()`
- `LOG_MODE=dev` → `zap.NewDevelopment()`

`NewFrom(*zap.Logger, tag)` — named child loggers for modules.

Env index: [adapter/config/README.md](../../config/README.md).
