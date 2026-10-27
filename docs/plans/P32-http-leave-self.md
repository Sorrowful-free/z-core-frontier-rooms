# P32 — HTTP leave: self-leave по reconnect-токену

## Контекст

P31 ввёл `DELETE /rooms/:id/peers/:peerID` (kick-anyone по API-key) —
загрилловано пост-фактум: leave должен быть **строго self** — клиент
уходит сам, доказательством peerID служит reconnect-токен (kind=2,
HMAC-claims несут roomID+peerID, не истекают). Kick по HTTP не нужен.

## Загрилленные решения (уже согласованы)

1. **`POST /rooms/:id/leave`, body `{"reconnect_token": "<base64url>"}`** —
   peerID и roomID из claims токена; `claims.RoomID` обязан совпадать с
   `:id` в path, mismatch → `401 invalid_credentials`.
2. **Кredential — только reconnect-токен** (`claims.Kind ==
   ClaimsKindReconnect`): одноразовый join-ticket после admit'а
   consumed/истекает — не подходит; join-token → `401`.
3. **`admission.Validate` (порт уже есть)** — claims без потребления;
   невалидный/чужой токен → `ErrInvalidToken` → `401 invalid_credentials`.
4. **Идемпотентно**: peer уже не в комнате → `LeaveRoom` вернёт nil
   (revoke tolerated) → `204`. Семантика DELETE-подобная: повторный
   leave — не ошибка.
5. **Kick-эндпоинт удалён чисто** (no-prerelease-compat): роут
   `DELETE /rooms/:id/peers/:peerID`, `LeavePeer`-handler,
   `parsePeerIDParam`/`peerIDParseAPIError` — всё вырезается.
   `codePeerNotFound` остаётся — `ErrPeerNotFound` может прийти из
   `LeaveRoom` (гонка) → 404.
6. Rate-limit — тот же `delete`-бакет (`HTTP_DELETE_RATE_PER_MIN`).
7. Peer'у не шлём кадр — его conn закрывается сервером (drain-aware
   Stop), клиент сам инициировал leave; комната узнаёт по обычному
   leave-потоку через policy.

## Файлы (скоуп)

### Prod (красная, override этим планом)

- `internal/delivery/http/rooms.go` — убрать `LeavePeer`/route
  peers/:peerID; добавить `LeaveSelf` + route `POST /rooms/:id/leave`;
  ctor — параметр `admission` (порт `port/admission`).
- `internal/delivery/http/dto.go` — `leaveRoomRequest{ReconnectToken}`.
- `internal/delivery/http/parse.go` — убрать `parsePeerIDParam`/
  `peerIDParseAPIError` (мёртвые).
- `cmd/rooms/main.go`, `tests/testutil/env/env.go` — wiring admission
  в `NewRoomsHandler`.

### Tests (зелёная)

- `tests/delivery/http/rooms_test.go` — заменить `TestHTTP_LeavePeer`:
  валидный reconnect-токен → 204; join-токен → 401; мусор → 401;
  токен чужой комнаты → 401; повторный leave → 204.

### Docs (зелёная)

- `docs/client-protocol/http.md` — роут `POST /rooms/:id/leave`,
  контракт токена, коды.
- `internal/delivery/http/README.md` — роут + семантика.
- `docs/plans/P31-empty-room-death.md` — решение 7 пометить как
  superseded этим планом (одна строка, история не переписывается).
- `docs/plans/P32-http-leave-self.md` — этот файл.

### Не трогаем

- admission порт/адаптер — `Validate` уже существует.
- Wire-протокол — leave остаётся HTTP-only (data-plane disconnect
  без изменений).

## Verification

1. `go build ./...`
2. `~/go/bin/golangci-lint run ./...`
3. `go test -race -shuffle=on -short -count=1 ./tests/...`
