# adapter/registry

In-memory `RoomRegistry`. Порт: `port/registry`.

- `CreateRoom` / `GetRoom` / `GetList` / `DeleteRoom`
- `CreateRoom`: re-check `rooms[id]` под write-локом; проигравшая в гонке комната — `Stop()` вне лока
- `DeleteRoom`: снятие с реестра под локом, `room.Stop()` вне лока (ошибка stop — в лог, комната остаётся снятой)
- Lifecycle `context.Context` на комнату
- `Shutdown` — остановка всех комнат при завершении процесса
- `NewRoomRegistry(ctx, roomFactory, logger, metrics)` — `metrics` (`port/metrics`, nil-safe): gauge `active_rooms` пересчитывается по `len(rooms)` при Create/Delete

Ошибки: `domain.ErrRoomNotFound`, `ErrRoomAlreadyExists`.
