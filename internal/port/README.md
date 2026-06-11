# port

Интерфейсы (границы приложения). Реализации — `internal/adapter/`.

| Пакет | Интерфейс | Реализация |
|-------|-----------|------------|
| `admission` | `Admission` | `adapter/admission` |
| `reservation` | `Reservation` | `adapter/reservation` |
| `registry` | `RoomRegistry` | `adapter/registry` |
| `realtime` | `Room`, `Peer`, `PeerFactory`, `RoomFactory` | `adapter/realtime` |
| `realtime/policy` | `RoomPolicy`, `RoomPolicyFactory` | `adapter/realtime/policy/state` |
| `realtime/codec` | codec interfaces | `adapter/realtime/codec` |
| `transport` | `Connection` | `adapter/transport/ws`, `enet` |
| `identity` | `Allocator` | `adapter/identity` |
| `logging` | `Logger` | `adapter/logging` |
| `httplimits` | `Limits` | `adapter/httplimits` |

Интерфейсы объявляет **потребитель** (use case / adapter), маленькие, без «на будущее».
