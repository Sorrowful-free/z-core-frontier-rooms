# P29 — room policy: authoritative-host (комната умирает с хостом)

## Контекст

Сейчас `RoomPolicyFactory.CreateRoomPolicy()` не принимает параметров —
каждая комната получает идентичный `StateRoomPolicy` с автоматической
master-миграцией (`masterMachine.onPeerLeft` → `electAndAssign`).

Цель: клиент (создатель комнаты) должен сообщать серверу, нужна ли
миграция хоста. Кейс — **авторитарный хост**: если хост (master-peer)
ушёл, комната считается мёртвой, перевыборы master'а не проводятся.

## Загрилленные решения (уже согласованы)

1. **Канал объявления** — first-class поле `policy` в `POST /rooms`
   (НЕ тикет: тикет — per-peer credential, выдаётся после создания
   комнаты; полиси — свойство комнаты и задаётся создателем).
2. **Семантика смерти** — master ушёл (leave И disconnect) → комната
   умирает сразу: `0x82 OpRoomClosed` всем оставшимся → disconnect всех
   peer'ов → unregister + revoke слотов. Grace-окно для хоста не
   применяется — авторитарный хост ушёл = матч окончен.
3. **Форма реализации** — ОТДЕЛЬНАЯ реализация `AuthoritativeHostPolicy`
   (не флаг в StateRoomPolicy), в пакете `policy/state` с доступом к
   внутренностям master-машины.
4. **Уведомление клиентов** — новый OpCode `0x82 OpRoomClosed`
   (room notifications band 0x80+, payload пустой) перед disconnect.
   Wire-изменение: требует обработки в Godot-клиенте.

## Зона / override

План с явным списком путей = зафиксированный скоуп override по
ai-companion (prod — красная зона). Список «файлы» ниже — единственное
разрешённое к правкам.

## Дизайн

### Объявление полиси

- `createRoomRequest` (delivery/http DTO): новое поле
  `policy string json:"policy,omitempty"`; пусто → дефолт.
- `domain.RoomPolicyKind` (новый тип, `internal/domain/`):

```go
type RoomPolicyKind string

const (
    RoomPolicyKindState              RoomPolicyKind = "state"               // дефолт: текущее поведение, master-миграция
    RoomPolicyKindAuthoritativeHost  RoomPolicyKind = "authoritative-host"  // уход хоста = смерть комнаты
)

func ParseRoomPolicyKind(s string) (RoomPolicyKind, error) // "" → RoomPolicyKindState
```

- Неизвестное значение → `domain.ErrInvalidPolicy` (новый sentinel) →
  HTTP `400 invalid_policy` (маппинг в `delivery/http/errors.go`).

### Проводка kind до фабрики (изменения сигнатур портов)

- `POST /rooms` handler: парсинг `req.Policy` → `ParseRoomPolicyKind`
  → `CreateUseCase.Create(..., policyKind)`.
- `port/registry.RoomRegistry.CreateRoom(ctx, id, capacity, attributes, policyKind)`
- `port/realtime.RoomFactory.CreateRoom(ctx, id, capacity, attributes, policyKind)`
- `port/realtime/policy.RoomPolicyFactory.CreateRoomPolicy(kind domain.RoomPolicyKind) RoomPolicy`
- `StateRoomPolicyFactory.CreateRoomPolicy(kind)`: `state` → текущий
  `NewStateRoomPolicy`; `authoritative-host` → `NewAuthoritativeHostPolicy`.
- Mocks: `go generate ./tests/mocks/...` (mock_room_policy_factory.go,
  room_factory, room_registry — перегенерация).

### AuthoritativeHostPolicy

- Новый тип в пакете `internal/adapter/realtime/policy/state/`
  (`authoritative_host_policy.go`): композиция над `*StateRoomPolicy`
  (тот же пакет — есть доступ к `masterMachine`).
- `OnLeave(peer)`: если уходящий peer — текущий master
  (`p.master.isMaster(peer.GetID())`) → **не** делегировать в
  `StateRoomPolicy.OnLeave` (перевыборы/патчи бессмысленны — комната
  умирает) и вернуть sentinel `domain.ErrRoomShouldClose` (новый
  sentinel в domain). Иначе — делегировать в `StateRoomPolicy.OnLeave`
  (обычная обработка ухода non-master peer'а).
- Остальные методы — делегирование без изменений.
- Reconnect (P20): при `authoritative-host` разрыв хоста идёт через
  тот же `OnLeave`-путь (DisconnectPeer → Leave) → комната умирает
  сразу, grace для хоста не применяется (слоты всё равно revoke'ятся
  при смерти комнаты). Resume других peer'ов бессмысленен — комнаты нет.

### Teardown при смерти комнаты

- `domain.ErrRoomShouldClose` — новый sentinel.
- Поток: `Room.Leave` → `policy.OnLeave` → sentinel вверх через
  `LeaveRoomUseCase` / `DisconnectPeerUseCase` (оба пути: явный leave и
  разрыв транспорта; также idle-eviction путь в `EvictIdlePeersUseCase`).
- Usecase при `errors.Is(err, domain.ErrRoomShouldClose)` выполняет
  teardown (новый общий шаг — например `CloseRoom` usecase или
  приватный хелпер, переиспользующий DeleteRoom-семантику):
  1. всем оставшимся peer'ам — кадр `OpRoomClosed` (0x82, payload
     пустой) через `peer.Deliver`;
  2. disconnect/stop каждого peer (identity-scoped teardown по P20-
     инвариантам реестров);
  3. unregister комнаты из registry;
  4. revoke слотов reservation.
- Гонки: уходивший master уже получил ответ/close; отправка `OpRoomClosed`
  — только оставшимся peer'ам до их disconnect.

### Wire: 0x82 OpRoomClosed

- `domain`: `OpRoomClosed OpCode = 0x82` (band room notifications 0x80+,
  не ошибка; payload пустой).
- Отправка всем оставшимся peer'ам до disconnect: WS — кадр + close;
  ENet — reliable packet + `DisconnectLater` (как join-reject).
- `.cursor/rules/join-error-opcodes.mdc` — добавить `0x82` в band
  room notifications 0x80+.
- `docs/client-protocol/errors.md` — секция room notifications:
  `0x82 OpRoomClosed` — комната закрыта сервером (authoritative-host:
  хост ушёл), после кадра соединение закрывается.
- `docs/client-protocol/{realtime,enet}.md` — упоминание в
  disconnect-семантике.
- **Клиент (Godot)** должен обработать `0x82` — зафиксировать в docs как
  обязательный обработчик; до реализации клиента разрыв виден как обрыв.

## Файлы (скоуп)

### Prod (красная, override этим планом)

- `internal/domain/` — `RoomPolicyKind` + `ParseRoomPolicyKind` +
  `ErrInvalidPolicy`, `ErrRoomShouldClose`, `OpRoomClosed` (новые файлы
  или в существующие: `type_aliases.go`/`claims.go`/`errors*.go` — по
  конвенции пакета)
- `internal/port/realtime/policy/room_policy_factory.go` — kind-параметр
- `internal/port/realtime/room_factory.go` — сигнатура + kind
- `internal/port/registry/room_registry.go` — сигнатура + kind
- `internal/adapter/realtime/policy/state/authoritative_host_policy.go` — НОВЫЙ
- `internal/adapter/realtime/policy/state/state_room_policy_factory.go` — dispatch по kind
- `internal/adapter/realtime/room_factory.go` — проброс kind
- `internal/adapter/registry/room_registry.go` — проброс kind
- `internal/delivery/http/dto.go` — поле `policy` в createRoomRequest
- `internal/delivery/http/rooms.go` — парсинг + маппинг `invalid_policy` → 400
- `internal/delivery/http/errors.go` — код `invalid_policy`
- `internal/usecase/room/create.go` — приём и проброс policyKind
- `internal/usecase/room/leave_room.go` — обработка `ErrRoomShouldClose` → teardown
- `internal/usecase/room/disconnect_peer.go` — то же на disconnect-пути
- `internal/usecase/room/evict_idle_peers.go` — то же на evict-пути
- Новый usecase/хелпер teardown'а комнаты (переиспользовать DeleteRoom-
  семантику: peers stop + unregister + revoke)
- `cmd/rooms/main.go` — если сигнатуры конструкторов меняются
- `tests/testutil/env/env.go` — wiring под новые сигнатуры

### Tests (зелёная)

- `tests/mocks/` — `go generate ./tests/mocks/...` (изменённые порты)
- `tests/delivery/http/` — create с `policy` (валидные значения, дефолт,
  `invalid_policy` → 400)
- `tests/usecase/room/` — Create с kind; leave/disconnect master'а в
  authoritative-host → teardown-вызовы (моки registry/reservation/peers)
- `tests/adapter/realtime/policy/state/` — `AuthoritativeHostPolicy`:
  master leave → `ErrRoomShouldClose` без re-elect; non-master leave →
  обычное поведение
- `tests/integration/` — e2e: create `authoritative-host` → 2 peers →
  host disconnect → `0x82` у выжившего → disconnect всех → room gone
  (`GET /rooms/:id` 404 / admit → 0x43)
- `tests/README.md` — при необходимости

### Docs (зелёная)

- `internal/delivery/http/README.md` — поле `policy` в POST /rooms
- `docs/client-protocol/http.md` — поле `policy`, `invalid_policy`
- `docs/client-protocol/errors.md` — `0x82 OpRoomClosed`
- `docs/client-protocol/{realtime,enet}.md` — disconnect/room-close
- `.cursor/rules/join-error-opcodes.mdc` — `0x82` в band 0x80+
- `internal/adapter/realtime/policy/state/README.md` — вторая policy
- `internal/domain/README.md` — `RoomPolicyKind`, sentinels, `OpRoomClosed`
- `internal/usecase/room/README.md` — teardown-поток
- `internal/delivery/{ws,enet}/README.md` — `0x82` при смерти комнаты
- `docs/adr/0011-authoritative-host-policy.md` — НОВЫЙ: room-level полиси,
  выбор при create, sentinel → teardown, `0x82`
- `GLOSSARY.md` — термины `authoritative host`/`host migration` при
  необходимости

### Не трогаем

- `docs/adr/0001–0010`, `docs/plans/P1–P28` — история.
- Codec — без изменений (кадр `OpCode+payload`, новый OpCode — не codec).
- Reservation/Admission wire-форматы тикетов — без изменений.

## Открытые детали для реализации

- Точное имя kind-строки в API: `authoritative-host` (предложение;
  альтернативы `authoritative`, `host-pinned`).
- `OpRoomClosed` payload: пустой (reason — в будущем при необходимости).
- Взаимодействие с `DeleteRoom` usecase: teardown переиспользует его
  шаги, но добавляет `OpRoomClosed` broadcast.
- Поведение `OnLeave` для non-master peer'ов при `authoritative-host` —
  делегируется в StateRoomPolicy без изменений (миграция просто не
  наступает: master всегда первый joiner, он же единственный «хост»).

## Verification

1. `go build ./...`
2. `~/go/bin/golangci-lint run ./...`
3. `go generate ./tests/mocks/...` после изменения портов
4. `go test -race -shuffle=on -short -count=1 ./tests/...`
5. `./scripts/enet-verify.sh build` + `vet` — tagged-тесты
6. CI-эквивалент `go test -race -shuffle=on -count=1 ./tests/...` перед пушем
