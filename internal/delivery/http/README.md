# delivery/http

REST control plane (Fiber v3), порт `:3000`.

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

`rooms.go` — handlers; `dto.go` — JSON types; `mapper.go` — summaries; `parse.go`, `errors.go`.
