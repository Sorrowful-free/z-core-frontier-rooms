# Тесты

Все тесты проекта лежат здесь, а не рядом с исходниками в `internal/` и `cmd/`.

Структура зеркалит слои приложения:

```text
tests/
  domain/           — unit домена
  usecase/room/     — Create, IssueTicket, JoinRoom, LeaveRoom, …
  adapter/          — admission, registry, realtime (с моками port)
  delivery/http/    — Fiber app.Test
  delivery/ws/      — при необходимости
  integration/      — сквозные сценарии
```

Пакеты — **внешние** (`package room_test`), импорт модуля `github.com/Sorrowful-free/z-core-frontier-rooms/...`.

Запуск из корня модуля:

```bash
go test ./tests/...
```

С race detector (рекомендуется для realtime):

```bash
go test -race ./tests/...
```

## Приоритет покрытия (когда появятся тесты)

1. `adapter/admission` — sign/verify, expired token, invalid MAC.
2. `usecase/room` — `IssueTicket`, `JoinRoom` (мок registry/admission), `LeaveRoom`.
3. `adapter/registry` — create/get/delete, ошибка старта комнаты.
4. `delivery/http` — маршруты REST после реализации handlers.

Подробнее о слоях и use case — [README.md](../README.md) в корне репозитория.
