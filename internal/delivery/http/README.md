# delivery/http

REST control plane (Fiber v3), порт `:3000`.

## Аутентификация

Все endpoints ниже защищены API key (`adapter/httpauth`), если не `HTTP_AUTH_DISABLED=true`.

Заголовки (один из):

- `Authorization: Bearer <HTTP_API_KEY>`
- `X-API-Key: <HTTP_API_KEY>`

Без ключа или при неверном ключе — `401` / `{"code":"unauthorized",…}`.

`/ws` и data plane не используют этот ключ (join — ticket).

## Endpoints

| Метод | Путь | Use case | Успех | Body запроса | Body ответа |
|-------|------|----------|-------|--------------|-------------|
| `POST` | `/rooms` | Create | `201` | `capacity`, `password?` | `id`, `peers` |
| `GET` | `/rooms` | GetList | `200` | — | `rooms[]` |
| `DELETE` | `/rooms/:id` | Delete | `204` | `password?` | — |
| `POST` | `/rooms/:id/tickets` | IssueTicket | `201` | `nick_name`, `password` | `token` (base64url) |

`nick_name` обязателен (`domain.ValidateNickName`). Ошибка: `400` / `invalid_nick_name`.

Пароль комнаты: если задан при create — нужен для IssueTicket и Delete.

## Ошибки JSON

`{"code":"…","message":"…"}` — маппинг в `errors.go` (`room_not_found`, `ticket_slot_held`, `invalid_nick_name`, …).

## Файлы

`rooms.go` — handlers; `auth_middleware.go` — API key; `dto.go` — JSON types; `mapper.go` — summaries; `parse.go`, `errors.go`.
