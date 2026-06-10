# Wire: state codec (room sync)

Бинарный контракт для игрового state между **Go-сервером** и **Godot-клиентом**.  
Реализация: [internal/adapter/realtime/codec/README.md](../internal/adapter/realtime/codec/README.md).  
OpCode таблица: [internal/delivery/errors/README.md](../internal/delivery/errors/README.md).  
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

**ID в map:** ключ map — единственный идентификатор сущности на wire. В теле value **не** дублируется `PeerID` / `EntityID` / `ComponentID` / `ValueId` (кроме ключа map).

**Отправитель input:** `RoomEvent.PeerID` на сервере; в payload input **нет** `peer_id`.

## Primitives

```text
u8   — 1 byte
u16  — 2 bytes BE
u32  — 4 bytes BE
i8   — 1 byte (room capacity)
i64  — 8 bytes BE (peer ping)

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
owner       u32
components  map ComponentID → component payload
```

Max components: **64** (`EntityStateMaxComponents`).

### Entity patch

```text
flags u8
  bit0 (1)   — owner u32 present
  bit2 (4)   — components map patch present

[owner u32]
[components patch]
```

Флаги: `EntityStateFlagsOwner = 1`, `EntityStateFlagsComponents = 2`.

## Peer (payload в room.Peers map)

```text
nick  string
ping  i64
```

Ключ map: `peer_id u32`. Max peers in room map: **255**.

### Peer patch

```text
flags u8
  bit0 — nick string
  bit1 — ping i64

[optional fields in flag order]
```

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

## Room full (`OpCodeFullState` = `0x03`)

```text
room_id   u32
capacity  i8
password  string
peers     map PeerID → peer payload
inputs    map PeerID → input payload
entities  map EntityID → entity payload
```

Лимиты: peers/inputs **255**, entities **65535**.

## Room patch (`OpCodePatchState` = `0x04`)

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

## OpCodes (state traffic)

| OpCode | Hex | Payload |
|--------|-----|---------|
| `OpCodePeerListFullState` | `0x01` | (зарезервирован; peers в room map) |
| `OpCodePeerListPatchState` | `0x02` | (зарезервирован) |
| `OpCodeFullState` | `0x03` | Room full (этот документ) |
| `OpCodePatchState` | `0x04` | Room patch |
| `OpCodeFullInput` | `0x05` | Input map |
| `OpCodePatchInput` | `0x06` | Input map patch |
| `OpCodeRpc` | `0x07` | RPC |

Ошибки join/reservation: `0x40+`, `0x70+` — см. [README.md](../README.md).

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
