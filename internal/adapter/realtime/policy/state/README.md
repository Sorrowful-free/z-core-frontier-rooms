# adapter/realtime/policy/state

`StateRoomPolicy` — authoritative state комнаты.

## Интерфейс RoomPolicy

- `OnStart` / `OnStop`
- `OnJoin` / `OnLeave`
- `OnMessage` — opcodes entities/input/RPC (см. `client_state_room_policy.go`)
- `TickIntervals`, `OnTickFullState`, `OnTickPatchState`

## Поведение

- **Master:** первый peer; failover — min `Ping()` ≥ 0, tie-break min `PeerID`; пустая комната → `master = nil`
- **Join:** full state новому peer; patch остальным; `NickName` из `peer.GetNickName()`
- **Ticks:** full state 4s, patch 50ms (дефолты); `0` в конструкторе → подстановка дефолтов
- **Master-only:** `FullEntities`, `PatchEntities`, input opcodes, RPC routing
- **Patch apply:** атомарно (`ApplyMapStatePatchCopy` + entity/component)
- **Ошибки:** sentinel `domain` → in-room OpCode отправителю

## Файлы

| Файл | Роль |
|------|------|
| `state_room_policy.go` | lifecycle, routing opcodes |
| `server_state_room_policy.go` | join, broadcast, pings |
| `client_state_room_policy.go` | master messages |
| `master_election.go` | выбор master |
| `state_room_policy_factory.go` | DI defaults |
