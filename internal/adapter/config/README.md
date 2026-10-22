# adapter/config

Сборка конфигурации процесса из env. См. [.cursor/rules/config.mdc](../../../.cursor/rules/config.mdc).

## API

- `LoadFromEnv() (*Config, error)` — единая точка загрузки при старте.

## Структура

`Config` содержит поля типов из adapter-пакетов модулей (`Logging`, `Server`, `Admission`, `HTTPAuth`, …, `Enet`).

## Env (process)

| Переменная | Обязательна | Default | Описание |
|------------|-------------|---------|----------|
| `LOG_MODE` | нет | `prod` | Корневой zap: `prod` или `dev` |
| `HTTP_ADDR` | нет | `:3000` | Адрес `fiber.App.Listen` |

Валидация — `logging/zap.RootConfig.Validate()`, `server.ServerConfig.Validate()`.

## Env (admission)

| Переменная | Обязательна | Default | Описание |
|------------|-------------|---------|----------|
| `ADMISSION_SECRET` | да | — | HMAC-секрет ticket, ≥ 32 байт; `dev-secret-change-me` запрещён |
| `ADMISSION_TTL` | нет | `1h` | TTL ticket (`time.ParseDuration`) |

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
| `HTTP_DELETE_RATE_PER_MIN` | нет | `30` | `DELETE /rooms/:id` на IP в минуту; `0` — без лимита |
| `HTTP_LIST_RATE_PER_MIN` | нет | `60` | `GET /rooms` на IP в минуту; `0` — без лимита |

Валидация — `httplimits.HTTPLimitsConfig.Validate()`.

## Зависимости

`adapter/config` импортирует adapter-модули (например `adapter/admission`). Обратный импорт запрещён.

## Env (reservation orphan sweep)

| Переменная | Обязательна | Default | Описание |
|------------|-------------|---------|----------|
| `RESERVATION_ORPHAN_ADMITTED_TTL` | нет | `30s` | Grace после Admit до revoke orphan без peer; `0` — сразу при sweep |
| `RESERVATION_ORPHAN_SWEEP_INTERVAL` | нет | `10s` | Период фонового sweep; `0` — отключить |

Валидация — `reservation.ReservationConfig.Validate()`.

## Env (enet listener)

Часть `LoadFromEnv()` → `Config.Enet` (тип `transport/enet.EnetConfig`).

| Переменная | Обязательна | Default | Описание |
|------------|-------------|---------|----------|
| `ENET_LISTEN_PORT` | нет | `7777` | UDP-порт ENet host; 1–65535 |
| `ENET_PEER_LIMIT` | нет | `64` | Макс. одновременных ENet peers |
| `ENET_ADMIT_TIMEOUT` | нет | `10s` | Deadline connect→admit; просроченный peer отключается |

Валидация — `transport/enet.EnetConfig.Validate()` (вызывается в `loadEnetFromEnv` и повторно в `cmd/rooms` после копирования transport-лимитов).

## Env (transport / data plane)

| Переменная | Обязательна | Default | Описание |
|------------|-------------|---------|----------|
| `TRANSPORT_MAX_INCOMING_FRAME_BYTES` | нет | `262144` (256 KiB) | Макс. входящий кадр WS/ENet и ENet admit-пакет; `0` — **без лимита** (см. ниже) |
| `TRANSPORT_ROOM_INCOMING_QUEUE` | нет | `128` | Буфер `room.incoming`; `0` — unbuffered; полный → drop |
| `TRANSPORT_PEER_OUTBOUND_QUEUE` | нет | `128` | Буфер `peer.outbound`; `0` — unbuffered; полный → drop |
| `TRANSPORT_ENET_INCOMING_QUEUE` | нет | `256` | Буфер ENet session; полный → drop кадра |
| `TRANSPORT_PEER_IDLE_TIMEOUT` | нет | `30s` | Окно живости `IsAlive` (WS lastSeen); `0` — отключить idle eviction (отрицательные — ошибка Validate) |
| `TRANSPORT_PEER_IDLE_SWEEP_INTERVAL` | нет | `10s` | Период фонового sweep idle peer'ов; `0` — отключить (отрицательные — ошибка Validate) |
| `TRANSPORT_WS_WRITE_DEADLINE` | нет | `10s` | Дедлайн записи WS-кадра (`SetWriteDeadline` перед `WriteMessage`); `0` — без дедлайна (отрицательные — ошибка Validate) |

`TRANSPORT_MAX_INCOMING_FRAME_BYTES=0` / `MaxIncomingFrameBytes=0` — **unlimited входящий кадр**: ни `SetReadLimit` (WS), ни проверка размера пакета (ENet) не применяются. Опасно (один клиент может сожрать память гигантским кадром) — только как явный opt-in, не для prod-дефолта.

Валидация — `transport.TransportConfig.Validate()`.

Wiring — `cmd/rooms`: `LoadFromEnv()` → `ws.NewWsConnectionFactory(..., cfg.Transport)`; `cfg.Enet` → `enetCfg`, затем `enetCfg.MaxIncomingFrameBytes = cfg.Transport.MaxIncomingFrameBytes`, `enetCfg.EnetIncomingQueueSize = cfg.Transport.EnetIncomingQueueSize`.
