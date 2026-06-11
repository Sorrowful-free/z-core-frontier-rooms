# adapter/config

Сборка конфигурации процесса из env. См. [.cursor/rules/config.mdc](../../../.cursor/rules/config.mdc).

## API

- `LoadFromEnv() (*Config, error)` — единая точка загрузки при старте.

## Структура

`Config` содержит поля типов из adapter-пакетов модулей (`Admission`, `HTTPAuth`, …).

## Env (admission)

| Переменная | Обязательна | Default | Описание |
|------------|-------------|---------|----------|
| `ADMISSION_SECRET` | да | — | HMAC-секрет ticket, ≥ 32 байт; `dev-secret-change-me` запрещён |
| `ADMISSION_TTL` | нет | `1h` | TTL ticket (`time.ParseDuration`) |
| `ADMISSION_PASSWORD` | нет | `""` | Глобальный пароль Issue; `""` — отключён |

Валидация полей admission — `admission.AdmissionConfig.Validate()`.

## Env (http auth)

| Переменная | Обязательна | Default | Описание |
|------------|-------------|---------|----------|
| `HTTP_API_KEY` | да* | — | API key REST; ≥ 16 байт; `Authorization: Bearer` или `X-API-Key` |
| `HTTP_AUTH_DISABLED` | нет | `false` | `true` — отключить auth (только local dev) |

\* Не обязателен, если `HTTP_AUTH_DISABLED=true`.

Валидация — `httpauth.HTTPAuthConfig.Validate()`.

## Зависимости

`adapter/config` импортирует adapter-модули (например `adapter/admission`). Обратный импорт запрещён.

Wiring — `cmd/rooms`: `cfg, _ := config.LoadFromEnv()` → `admission.NewAdmission(cfg.Admission)`, `RegisterRoutes(app, cfg.HTTPAuth)`.
