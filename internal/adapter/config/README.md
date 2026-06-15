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

## Env (http limits)

| Переменная | Обязательна | Default | Описание |
|------------|-------------|---------|----------|
| `MAX_ROOMS` | нет | `500` | Макс. комнат в процессе; `0` — без лимита |
| `HTTP_CREATE_RATE_PER_MIN` | нет | `30` | `POST /rooms` на IP в минуту; `0` — без лимита |
| `HTTP_ISSUE_RATE_PER_MIN` | нет | `60` | `POST /rooms/:id/tickets` на IP в минуту; `0` — без лимита |

Валидация — `httplimits.HTTPLimitsConfig.Validate()`.

## Зависимости

`adapter/config` импортирует adapter-модули (например `adapter/admission`). Обратный импорт запрещён.

## Env (reservation orphan sweep)

| Переменная | Обязательна | Default | Описание |
|------------|-------------|---------|----------|
| `RESERVATION_ORPHAN_ADMITTED_TTL` | нет | `30s` | Grace после Admit до revoke orphan без peer; `0` — сразу при sweep |
| `RESERVATION_ORPHAN_SWEEP_INTERVAL` | нет | `10s` | Период фонового sweep; `0` — отключить |

Валидация — `reservation.ReservationConfig.Validate()`.

Wiring — `cmd/rooms`: `LoadFromEnv()` → `httplimits.New(cfg.HTTPLimits)` → `NewCreateUseCase(..., limits)`, `RegisterRoutes(app, cfg.HTTPAuth, limits)`, `StartOrphanAdmittedSweep(..., cfg.Reservation)`.
