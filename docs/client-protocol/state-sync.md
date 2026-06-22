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

## Replace (reconnect)

Если peer с тем же `peer_id` из ticket уже в комнате (обрыв WS без leave), повторный join с тем же ticket выполняет **Replace** — старое соединение заменяется новым.

- Успех: как обычный join (`0x01` full state).
- Ошибка replace: join reject `0x45` `OpReplaceFailed`; старый peer **остаётся** в комнате.

Для нового подключения после disconnect обычно нужен **новый** issue ticket (новый `peer_id`), если политика слотов не допускает повторный admit.

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
