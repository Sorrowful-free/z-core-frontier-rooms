# Wire: state codec (room sync)

Бинарный контракт для игрового state между **Go-сервером** и **Godot-клиентом**.  
E2e-поток (HTTP → join → sync): [client-protocol/README.md](client-protocol/README.md).  
Реализация: [internal/adapter/realtime/codec/README.md](../internal/adapter/realtime/codec/README.md).  
OpCode ошибок: [internal/delivery/errors/README.md](../internal/delivery/errors/README.md).  
Кадр транспорта: `domain.Frame` = `OpCode` (1 байт) + `Payload` (этот документ описывает **payload** state/input/rpc).

## Общие правила

| Правило | Значение |
|---------|----------|
| Порядок байт | **Big-endian** для всех multi-byte чисел |
| Map (full) | `count u16` + `count` × (`key` + `value`) |
| Map (patch) | `added_count u16` + entries + `updated_count u16` + entries + `removed_count u16` + keys |
| Value payload | `len u8` + `len` raw bytes (opaque, без Go `binary` slice encoding) |
| String | `len u8` + UTF-8 bytes (`password`, `nick`) |
| Пустой / отсутствует ID | `PeerID(0)`, `RoomID(0)` и т.д. — невалидны; на wire `peer_id u32 = 0` = «нет целевого peer» (RPC) |
| Лимиты | См. константы в codec; при превышении encode/decode возвращает ошибку |

**ID в map:** ключ map — единственный instance id на wire. В теле value **не** дублируется `PeerID` / `EntityID` (u16) / `ComponentID` / `ValueId` (кроме ключа map). Поле `entity_type_id u8` в entity payload — тип сущности, не instance id.

**Отправитель input:** `RoomEvent.PeerID` на сервере; в payload input **нет** `peer_id`.

## Primitives

```text
u8   — 1 byte
u16  — 2 bytes BE
u32  — 4 bytes BE
i8   — 1 byte (room capacity)
i64  — 8 bytes BE (peer ping)
bool — 1 byte (0 = false, 1 = true; Go `binary.Write` для bool)

string — u8 len + bytes (max len 255)
value  — u8 len + bytes (max len 255)
```

## Value map entry

```text
value_id  u8
value     value (len + bytes)
```

`ValueState` в домене = opaque `[]byte`.

## Component (payload в entity map)

Только map values (см. Value map). Max entries: **32** (`ComponentStateMaxValues`).

```text
component_id u8   — ключ в entity.Components
payload      — writeComponentState (values map)
```

## Entity (payload в room map)

```text
entity_type_id u8   — тип сущности (игровой enum на клиенте)
owner          u32
components     map ComponentID → component payload
```

Ключ map: `EntityID u16` — instance id. Поле `entity_type_id` в payload — **тип** сущности, не дублирует ключ map.

Max components: **64** (`EntityStateMaxComponents`).

### Entity patch

```text
flags u8
  bit0 (1) — entity_type_id u8 present
  bit1 (2) — owner u32 present
  bit2 (4) — components map patch present

[entity_type_id u8]
[owner u32]
[components patch]
```

Флаги: `EntityStateFlagsEntityTypeID = 1`, `EntityStateFlagsOwner = 2`, `EntityStateFlagsComponents = 4`.

## Peer (payload в room.Peers map)

```text
nick      string
is_master bool
ping      i64
```

Ключ map: `peer_id u32`. Max peers in room map: **255**.

### Peer patch

```text
flags u8
  bit0 (1) — nick string
  bit1 (2) — is_master bool
  bit2 (4) — ping i64

[optional fields in flag order]
```

Флаги: `PeerStateFlagsNickName = 1`, `PeerStateFlagsIsMaster = 2`, `PeerStateFlagsPing = 4`.

## Input (opcode `0x05` full / `0x06` patch)

Только map values. Max values: **32** (`InputStateMaxValues`).

Patch = map patch (added/updated/removed value entries).

## RPC (opcode `0x07`)

```text
rpc_id    u8
target    u8   — 0 none, 1 peer, 2 master, 3 all
values    map ValueId → value
peer_id   u32  — 0 = нет целевого peer
```

Max values: **32** (`RpcStateMaxValues`).

## Room full (`OpCodeFullState` = `0x01`)

```text
room_id   u32
capacity  i8
password  string
peers     map PeerID → peer payload
inputs    map PeerID → input payload
entities  map EntityID → entity payload
```

Лимиты: peers/inputs **255**, entities **65535**.

## Room patch (`OpCodePatchState` = `0x02`)

```text
flags u8
  bit0 (1)   — capacity i8
  bit1 (2)   — password string
  bit2 (4)   — peers map patch
  bit3 (8)   — inputs map patch
  bit4 (16)  — entities map patch

[optional sections in bit order]
```

Секция пишется/читается **только если** соответствующий bit в `flags`.

## Entities standalone (`OpCodeFullEntities` = `0x03` / `OpCodePatchEntities` = `0x04`)

Отдельные opcodes **только от master** к серверу. Payload — **не** room header, только entities map:

| OpCode | Hex | Payload |
|--------|-----|---------|
| `OpCodeFullEntities` | `0x03` | `entities` map (full), как секция entities в room full |
| `OpCodePatchEntities` | `0x04` | `entities` map patch, как секция entities в room patch |

После приёма сервер рассылает `OpCodePatchState` (`0x02`) остальным peer (master исключается из broadcast entities).

## OpCodes (state traffic)

| OpCode | Hex | Направление | Payload |
|--------|-----|-------------|---------|
| `OpCodeFullState` | `0x01` | server → client | Room full |
| `OpCodePatchState` | `0x02` | server → client | Room patch |
| `OpCodeFullEntities` | `0x03` | master → server | Entities map only |
| `OpCodePatchEntities` | `0x04` | master → server | Entities map patch only |
| `OpCodeFullInput` | `0x05` | client → server | Input map |
| `OpCodePatchInput` | `0x06` | client → server | Input map patch |
| `OpCodeRpc` | `0x07` | client → server → relay | RPC |

Источник истины: [internal/domain/state/opcodes.go](../internal/domain/state/opcodes.go).

Ошибки join/reservation/in-room: `0x40+` — см. [client-protocol/errors.md](client-protocol/errors.md).

## Godot (кратко)

```gdscript
# u32 BE из PackedByteArray b at offset
func read_u32_be(b: PackedByteArray, off: int) -> int:
    return (b[off] << 24) | (b[off+1] << 16) | (b[off+2] << 8) | b[off+3]

# value после value_id
func read_value(b: PackedByteArray, off: int) -> Dictionary:
    var n = b[off]
    off += 1
    return {"bytes": b.slice(off, off + n), "next": off + n}
```

Константы флагов и лимиты держите в синхроне с Go (`room_state_codec.go`, `entity_codec.go`, …).

## Тесты

Round-trip: `go test ./tests/adapter/realtime/codec/...`
