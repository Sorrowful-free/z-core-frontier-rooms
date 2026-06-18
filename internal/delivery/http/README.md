# delivery/http

REST control plane (Fiber v3), адрес — env `HTTP_ADDR` (default `:3000`).

## Аутентификация

Все endpoints ниже защищены API key (`adapter/httpauth`), если не `HTTP_AUTH_DISABLED=true`.

Заголовки (один из):

- `Authorization: Bearer <HTTP_API_KEY>`
- `X-API-Key: <HTTP_API_KEY>`

Без ключа или при неверном ключе — `401` / `{"code":"unauthorized",…}`.

`/ws` и data plane не используют этот ключ (join — ticket).

## Лимиты

| Механизм | Где | Ответ |
|----------|-----|-------|
| Rate limit create/issue | middleware per IP | `429` / `rate_limited` |
| Max rooms на инстанс | `Create` use case | `503` / `rooms_limit_reached` |

Env — [adapter/httplimits/README.md](../../adapter/httplimits/README.md).

## Endpoints

| Метод | Путь | Use case | Успех | Body запроса | Body ответа |
|-------|------|----------|-------|--------------|-------------|
| `POST` | `/rooms` | Create | `201` | `capacity` (1…`domain.MaxRoomCapacity`), `password?` | `id`, `peers` |
| `GET` | `/rooms` | GetList | `200` | — | `rooms[]` |
| `DELETE` | `/rooms/:id` | Delete | `204` | `password?` | — |
| `POST` | `/rooms/:id/tickets` | IssueTicket | `201` | `nick_name`, `password` | `token` (base64url) |

`nick_name` обязателен (`domain.ValidateNickName`). Ошибка: `400` / `invalid_nick_name`.

Пароль комнаты: если задан при create — нужен для IssueTicket и Delete.

## Ошибки JSON

`{"code":"…","message":"…"}` — маппинг в `errors.go` (`room_not_found`, `ticket_slot_held`, `invalid_nick_name`, `invalid_capacity`, …).

`POST /rooms`: `capacity` ≤ 0 или > `domain.MaxRoomCapacity` (127) → `400` / `invalid_capacity`.

## Файлы

`rooms.go` — handlers; `auth_middleware.go` — API key; `rate_limit_middleware.go` — per-IP limits; `dto.go` — JSON types; `mapper.go` — summaries; `parse.go`, `errors.go`.
