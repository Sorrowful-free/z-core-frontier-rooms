# Room policy: authoritative-host (комната умирает с хостом)

До P29 каждая комната получала идентичный `StateRoomPolicy` с автоматической master-миграцией: уход master'а вёл к `electAndAssign` среди оставшихся. Для сценария «авторитарный хост» (сервер матча живёт в клиенте-хосте) это неприменимо: ушёл хост — матч окончен, перевыборы бессмысленны.

## Decision

- **Объявление полиси — first-class поле `policy` в `POST /rooms`** (`domain.RoomPolicyKind`: `state` дефолт / `authoritative-host`), а не ticket: тикет — per-peer credential, полиси — свойство комнаты, задаваемое создателем. Неизвестное значение → `ErrInvalidPolicy` → HTTP `400 invalid_policy`.
- **Отдельная реализация `AuthoritativeHostPolicy`** (композиция над `*StateRoomPolicy`, тот же пакет `adapter/realtime/policy/state`), не флаг: `OnLeave` уходящего master'а возвращает sentinel `domain.ErrRoomShouldClose` вместо делегирования в state-перевыборы; остальное поведение — делегирование.
- **Dispatch по kind — композитная `KindRoomFactory`** (`adapter/realtime`): leaf `realtime.RoomFactory` на каждую policy factory (`StateRoomPolicyFactory` / `AuthoritativeHostPolicyFactory`). Port `policy.RoomPolicyFactory.CreateRoomPolicy()` не расширяется — kind несут только `port/realtime.RoomFactory` и `port/registry.RoomRegistry`.
- **Смерть комнаты — teardown в usecase**: `LeaveRoom`/`DisconnectPeer` при `ErrRoomShouldClose` вызывают `closeRoom` (`usecase/room/close_room.go`): `OpRoomClosed` (`0x82`, payload пустой) всем оставшимся через `peer.Deliver` → `registry.DeleteRoom` → `reservation.UnregisterRoom` (ревокает и слот ушедшего) → `peer.Stop` каждого. Grace-окно хосту не применяется; resume невозможен — комнаты нет.
- **Wire:** `OpRoomClosed = 0x82` (band room notifications 0x80+, не ошибка), за кадром disconnect; на ENet порядок жёсткий (reliable packet + `DisconnectLater` в FIFO host loop'а), на WS — best-effort флеш (см. Consequences).

## Considered Options

- **Флаг `noMasterMigration` в StateRoomPolicy** — отклонено: ветвление внутри `OnLeave` размыло бы контракт policy; отдельный тип сохраняет обе реализации читаемыми.
- **Dispatch внутри `StateRoomPolicyFactory.CreateRoomPolicy(kind)`** — отклонено: расширяло бы порт фабрики kind'ом и смешивало две полиси в одной фабрике; композитная фабрика комнат чище.
- **Grace-окно для хоста (P20-модель)** — отклонено: авторитарный хост ушёл = матч окончен; держать слот/peerID под resume бессмысленно.
- **Reason-payload у `0x82`** — отложено: сейчас причина одна (host gone), payload пустой.

## Consequences

- `0x82` обязателен к обработке в Godot-клиенте; до её реализации закрытие комнаты видно как обрыв. Комната после смерти исчезает из registry и reservation целиком — `GET /rooms`/`admit` → `0x43`.
- На WS `0x82` доставляется best-effort: `peer.Deliver` коммитит кадр в write-loop, а `peer.Stop→conn.Close` идёт из другой горутины — `closeRoom` выстраивает порядок (Deliver до unregister'а, Stop последним + scheduling yield), но гарантия «send до close» требует drain-aware close на уровне `Peer` — отдельная работа.
- `room.Leave` логирует `ErrRoomShouldClose` как `policy on leave failed` (не входил в скоуп плана) — разовая строка на смерть комнаты.
- Evict-путь (`EvictIdlePeersUseCase`) покрыт без правок: он делегирует в `DisconnectPeer`.
