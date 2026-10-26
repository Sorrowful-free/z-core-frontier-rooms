# State sync

Authoritative room state на сервере (`StateRoomPolicy`). Клиент **принимает** full/patch от сервера и **отправляет** input, entities (master), RPC.

## OpCode и направление

| OpCode | Hex | Направление | Кто может отправлять |
|--------|-----|-------------|----------------------|
| FullState | `0x01` | server → all | Сервер |
| PatchState | `0x02` | server → all | Сервер |
| FullEntities | `0x03` | client → server | **Только master** |
| PatchEntities | `0x04` | client → server | **Только master** |
| FullInput | `0x05` | client → server | Любой peer |
| PatchInput | `0x06` | client → server | Любой peer |
| Rpc | `0x07` | client → relay | Любой peer |

Payload: [wire-state-codec.md](../wire-state-codec.md).

**Breaking change (P6d, D2):** `password` удалён из Room full / Room patch; флаги patch сдвинуты (`peers = 2`, `inputs = 4`, `entities = 8`). Payload с лишними trailing bytes — ошибка на сервере.

## Server → client

### On join

- Новому peer: `0x01` FullState (полный snapshot).
- Остальным: `0x02` PatchState (добавление peer в maps).

### Periodic ticks

| Интервал | Кадр | Default |
|----------|------|---------|
| Full state | `0x01` | каждые **4 s** |
| Patch state | `0x02` | каждые **50 ms** |

Клиент должен уметь применять patch поверх локального state (merge maps по правилам wire-doc).

### После client → server

| Клиент шлёт | Сервер делает |
|-------------|---------------|
| `0x05` / `0x06` input | Обновляет `Inputs[peer_id]`, рассылает patch всем |
| `0x03` / `0x04` entities (master) | Обновляет `Entities`, patch всем **кроме master** |
| `0x07` RPC | Relay по target (см. ниже) |

## Master

- **Первый** peer в комнате становится master.
- **Failover:** среди peer с `ping >= 0` — минимальный ping; при равенстве — минимальный `peer_id`. Если ping ни у кого не известен (`-1`) — минимальный `peer_id`.
- Перевыбор происходит не только при уходе master: если прошлая попытка election упала, сервер переизбирает master на следующем patch-тике или лениво при входящем `0x07` RPC с `target=master` — `is_master` может поменяться в любом patch.
- `is_master` никогда не указывает на мёртвого peer: сервер пишет флаг только после проверки, что избранный peer жив (validate-first), а кандидаты, уже отсутствующие в комнате, выборами пропускаются.
- В peer state на wire: поле `is_master` (bool).
- Только master может слать `0x03` / `0x04`. Иначе сервер шлёт in-room `0x61` `OpInRoomNotMaster` отправителю.

### Godot: проверка master

```gdscript
func is_local_master(room_state: Dictionary, local_peer_id: int) -> bool:
    var peers = room_state.get("peers", {})
    if not peers.has(local_peer_id):
        return false
    return peers[local_peer_id].get("is_master", false)
```

## Input

- `peer_id` **не** в payload — сервер берёт из соединения.
- Любой peer может слать full (`0x05`) или patch (`0x06`) своего input map.
- Значения — opaque bytes (`ValueId` → blob); семантику задаёт игра.

## Entities (master only)

OpCodes `0x03` / `0x04` несут **только** entities map (не полный room). После приёма сервер merge в authoritative state и шлёт room patch остальным.

Каждая entity в map содержит `entity_type_id u8` — тип сущности для spawn на клиенте (игровой enum). Instance id — ключ map `EntityID u16`.

## RPC (`0x07`)

```text
rpc_id  u8
target  u8
values  map
peer_id u32
```

| `target` | Имя | Доставка |
|----------|-----|----------|
| `0` | none | Ошибка `0x63` invalid RPC |
| `1` | peer | Одному `peer_id` из payload (должен быть в комнате) |
| `2` | master | Текущему master |
| `3` | all | Всем peer **кроме отправителя** |

Сервер **relay** — пересылает тот же payload (`0x07`) получателям; не интерпретирует `rpc_id` / values.

## Reconnect (P20)

Модель reconnect (P20): разрыв транспорта **удаляет** peer'а из комнаты обычным leave-путём — остальным уходит remove-патч и `0x80` `OpPeerDisconnected`; если ушёл master, election идёт сразу на leave. Идентичность при этом живёт: admitted-слот reservation держит capacity и `peer_id` в grace-окне `RESERVATION_RECONNECT_TTL` (серверный, default `30s`, от `lastSeen` слота — момента разрыва).

- **Resume** — повторный admit с `reconnect_token` (v4) вместо join-тикета (`?token=` / первый ENet-пакет): тот же `peer_id`, nick — из слота (в токене его нет). Успех — как обычный join: вернувшемуся `0x01` FullState, остальным `0x81` `OpPeerReconnected`. Поток и reject-коды: [realtime.md](realtime.md#reconnect-p20), [errors.md](errors.md).
- **Мастерство не возвращается:** election при disconnect уже отдал `is_master` другому — вернувшийся вступает обычным peer и становится master только по общим правилам выше (первый в опустевшей комнате / failover).
- **Replace (newest-wins):** resume при **живом** старом conn вытесняет его — старое соединение получает разрыв; grace-дедлайн на этот путь не действует.
- Ошибки: `0x45` `OpReplaceFailed` / `0x51` `OpPeerStartFailed` — как при join; просроченная grace — `0x74` `OpReservationExpired` (если sweep уже отозвал слот — `0x73`), дальше только новый issue ticket (новый `peer_id`).

## Рекомендуемая структура Godot

```text
RoomClient          — HTTP + WS lifecycle
WireCodec           — encode/decode payload (wire-state-codec)
RoomStateModel      — локальная копия maps peers/inputs/entities
StateSync           — apply full/patch, diff для отправки input
MasterLogic         — entities update только если is_master
RpcHandler          — encode/decode 0x07 по игровым RpcID
```

Источник поведения сервера: `internal/adapter/realtime/policy/state/`.
