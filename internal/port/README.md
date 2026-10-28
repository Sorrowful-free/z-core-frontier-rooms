# port

Interfaces (application boundaries). Implementations — `internal/adapter/`.

| Package | Interface | Implementation |
|---------|-----------|----------------|
| `admission` | `Admission` | `adapter/admission` |
| `reservation` | `Reservation` | `adapter/reservation` |
| `registry` | `RoomRegistry` | `adapter/registry` |
| `realtime` | `Room`, `Peer`, `PeerFactory`, `RoomFactory`, `EventSink` | `adapter/realtime` |
| `realtime/policy` | `RoomPolicy`, `RoomPolicyFactory` | `adapter/realtime/policy/state` |
| `realtime/codec` | codec interfaces | `adapter/realtime/codec` |
| `transport` | `Connection` | `adapter/transport/ws`, `enet` |
| `identity` | `Allocator` | `adapter/identity` |
| `logging` | `Logger` | `adapter/logging` |
| `httplimits` | `Limits` | `adapter/httplimits` |
| `metrics` | `Metrics` | `adapter/metrics` |

Interfaces are declared by the **consumer** (use case / adapter), kept small, nothing "for the future".
