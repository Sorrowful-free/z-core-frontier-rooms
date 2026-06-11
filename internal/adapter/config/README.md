# adapter/config

Сборка конфигурации процесса из env. См. [.cursor/rules/config.mdc](../../../.cursor/rules/config.mdc).

## API

- `LoadFromEnv() (*Config, error)` — единая точка загрузки при старте.

## Структура

`Config` содержит поля типов из adapter-пакетов модулей (`Admission`, позже `HTTP`, …).

## Env (admission)

| Переменная | Обязательна | Default | Описание |
|------------|-------------|---------|----------|
| `ADMISSION_SECRET` | да | — | HMAC-секрет ticket, ≥ 32 байт; `dev-secret-change-me` запрещён |
| `ADMISSION_TTL` | нет | `1h` | TTL ticket (`time.ParseDuration`) |
| `ADMISSION_PASSWORD` | нет | `""` | Глобальный пароль Issue; `""` — отключён |

Валидация полей admission — `admission.AdmissionConfig.Validate()`.

## Зависимости

`adapter/config` импортирует adapter-модули (например `adapter/admission`). Обратный импорт запрещён.

Wiring — `cmd/rooms`: `cfg, _ := config.LoadFromEnv()` → `admission.NewAdmission(cfg.Admission)`.
