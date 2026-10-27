# adapter/reservation

In-memory слоты комнаты. Порт: `port/reservation`.

## Блокировки

- `roomsMu` (`sync.RWMutex`) — только map `roomID → *reservationRoom` (register / unregister / lookup).
- `reservationRoom.mu` — слоты и пароль одной комнаты (`Reserve`, `Admit`, `Revoke`, `State`, `ListAdmittedPeers`, `VerifyRoomPassword`).

Операции в разных комнатах не сериализуют друг друга.

## Методы

| Метод | Кто вызывает |
|-------|----------------|
| `RegisterRoom` / `UnregisterRoom` | Create / Delete |
| `Reserve` | IssueTicket |
| `Admit` | JoinRoom |
| `Revoke` | LeaveRoom, откат Issue/Join, orphan cleanup в Issue, sweep |
| `MarkDisconnected` | DisconnectPeer (начало grace, P20) |
| `SlotDetails` | ReconnectRoom (read-only lastSeen для deadline-гейта, P24) |
| `Reconnect` | ReconnectRoom (атомарная проверка + refresh lastSeen, P20) |
| `State` | IssueTicket (orphan) |
| `ListAdmittedPeers` | SweepOrphanAdmitted, `closeRoomIfEmpty` (P31) |
| `ListRoomSlots` | SweepEmptyRooms (P31) — read-only листинг слотов **всех** состояний (`domain.RoomSlotDetail`: `PeerID`, `State`, `ExpiresAt`, `LastSeen`); нужен предикату «ни admitted, ни unexpired reserve» |
| `VerifyRoomPassword` | IssueTicket, Delete |

## Слоты

`none` → `reserved` (TTL из admission) → `admitted` (до Leave / orphan cleanup / resume).

- `Reserve(ctx, roomID, peerID, nickName, expiresAt)`: один outstanding `reserved`-слот на nick в комнате — повторный Reserve с тем же nick, пока слот не `admitted`/не удалён, → `ErrTicketSlotHeld`.
- `Admit` одноразовый: повторный `Admit` на `admitted`-слоте → `ErrReservationAlreadyAdmitted`.
- `Revoke` без peer в map → `nil` (идемпотентно).
- Просроченные `reserved` — lazy sweep при `Reserve`.
- `admitted` хранит `lastSeen` — единый timestamp жизни слота (P20): `MarkDisconnected` обновляет его на момент разрыва (слот в grace — capacity удерживается), `Reconnect` атомарно под `room.mu` проверяет `admitted` + обновляет lastSeen + возвращает слот с `NickName` (гонка sweep-revoke ↔ resume закрыта). Слот нет или не `admitted` → `ErrReservationNotReserved`.
- `SlotDetails` (P24) — тот же сериализованный путь, что у `Reconnect`, но **без** refresh'а `lastSeen`: честный lastSeen для дедлайн-гейта в `ReconnectRoomUseCase` (отклонённый resume не продлевает grace).
- Orphan (нет peer в room) — фоновый sweep (`SweepOrphanAdmittedUseCase`) после `lastSeen + ReconnectTTL` (grace-expiry). Тот же дедлайн enforced и на resume: `SlotDetails` + гейт в UC → `ErrReservationExpired` (P24).
- Never-joined/empty-комнаты (P31) — `SweepEmptyRoomsUseCase` на том же интервале `RESERVATION_ORPHAN_SWEEP_INTERVAL` (`0` отключает оба sweep'а): для комнат с `peers==0` читает `ListRoomSlots` и убивает комнату, когда ни admitted, ни unexpired reserve не осталось.
- Пароль комнаты хранится как SHA-256 (`passwordHash`), сверка — `subtle.ConstantTimeCompare`; plaintext не хранится. `RegisterRoom` валидирует capacity в `[1, domain.MaxRoomCapacity]`.
- Env: `RESERVATION_RECONNECT_TTL` (default `30s`), `RESERVATION_ORPHAN_SWEEP_INTERVAL` (default `10s`; `0` — отключить **оба** sweep'а: orphan admitted и empty rooms, P31).
- `NewReservation(logger, metrics)` — `metrics` (`port/metrics`, nil-safe): counters переходов слотов `reserve`/`admit`/`revoke` (revoke — любое реальное удаление слота, P19b: `Revoke`, expired-слот в `Admit`, lazy sweep просроченных при `Reserve`) и gauge живых слотов `reservation_active_slots` (reserve +1, каждое удаление -1, tombstone комнаты при `UnregisterRoom` -len(peers)).

Бенчмарки (per-room vs legacy global mutex): `tests/adapter/reservation/reservation_bench_test.go`, см. [tests/README.md](../../../tests/README.md).

Ошибки: `domain/reservation_error.go`. Wire `0x70–0x7F` — `delivery/errors`.
