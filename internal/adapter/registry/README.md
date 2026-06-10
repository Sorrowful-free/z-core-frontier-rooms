# adapter/registry

In-memory `RoomRegistry`. Порт: `port/registry`.

- `CreateRoom` / `GetRoom` / `GetList` / `DeleteRoom`
- Lifecycle `context.Context` на комнату
- `Shutdown` — остановка всех комнат при завершении процесса

Ошибки: `domain.ErrRoomNotFound`, `ErrRoomAlreadyExists`.
