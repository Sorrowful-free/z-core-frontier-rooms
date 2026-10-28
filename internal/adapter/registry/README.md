# adapter/registry

In-memory `RoomRegistry`. Port: `port/registry`.

- `CreateRoom` / `GetRoom` / `GetList` / `DeleteRoom`
- `CreateRoom`: re-check `rooms[id]` under the write lock; the room that loses the race — `Stop()` outside the lock
- `DeleteRoom`: removal from the registry under the lock, `room.Stop()` outside the lock (a stop error — to the log, the room stays removed)
- Lifecycle `context.Context` per room
- `Shutdown` — stops all rooms on process shutdown
- `NewRoomRegistry(ctx, roomFactory, logger, metrics)` — `metrics` (`port/metrics`, nil-safe): the `active_rooms` gauge is recomputed from `len(rooms)` on Create/Delete

Errors: `domain.ErrRoomNotFound`, `ErrRoomAlreadyExists`.
