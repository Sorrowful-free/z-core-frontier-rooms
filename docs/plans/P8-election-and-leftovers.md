# P8 — master election semantics + audit leftovers

Скоуп зафиксирован гриллингом. Красная зона: править только перечисленные пути.

## Master election

- `internal/adapter/realtime/policy/state/master_election.go`
  - `electMasterPeerID`: `ErrPeerNotFound` от `room.GetPeer` — пропустить кандидата (выбор среди живых); прочие ошибки — fail выбор
  - `assignMaster`: `GetPeer(masterID)` до записи `IsMaster`-флагов — state не рекламирует мёртвого мастера
- `internal/adapter/realtime/policy/state/server_state_room_policy.go`
  - дедуп логов: `Error` один раз на masterless-эпизод, `Info` при восстановлении (флаг/счётчик в policy)
- `tests/adapter/realtime/policy/state/` — тесты: skip-stale, отсутствие дивергенции флагов, переработка `failMemberGetPeer` под новую семантику
- `GLOSSARY.md`, `docs/adr/0005-master-election-semantics.md`, `internal/adapter/realtime/policy/state/README.md`, `docs/client-protocol/state-sync.md`

## Audit leftovers

- `internal/adapter/realtime/peer.go` — fatal deliver-ошибка в `processIncomingEvents` → `connection.Close()` перед return
- `internal/delivery/errors/join_reject.go` — `IsJoinRejectOpCode` → `0x40–0x5F || 0x70–0x7F`; тест `tests/delivery/errors/wire_test.go`
- `internal/adapter/transport/transport_config.go` — knob `WSWriteDeadline` (default ~10s); `internal/adapter/transport/ws/connection.go` — `SetWriteDeadline` перед `WriteMessage`
- `internal/delivery/http/dto.go` — убрать `createRoomRequest.ID`; синк `docs/client-protocol/http.md`
- `internal/delivery/http/errors.go` — `ErrInvalidRoomID` → 400 `invalid_room_id`, `ErrInvalidPeerID` → 400 `invalid_peer_id`
- `internal/adapter/config/README.md` — задокументировать `MaxIncomingFrameBytes=0` = безлимит
- `.env.example` — добавить `ENET_LISTEN_PORT` / `ENET_PEER_LIMIT` / `ENET_ADMIT_TIMEOUT`

## Отдельные планы / deferred

- `P9-connection-factory` — унификация `EnetConnectionFactory` под тегами + vendor-типы в port
- dispatch-to-host-loop вместо `apiMu`
- консолидация рукописных стабов под gomock
