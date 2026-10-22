# adapter/realtime/policy/state

`StateRoomPolicy` — authoritative state комнаты.

## Интерфейс RoomPolicy

- `OnStart` / `OnStop`
- `OnJoin` / `OnLeave`
- `OnMessage` — opcodes entities/input/RPC (см. `client_state_room_policy.go`)
- `TickIntervals`, `OnTickFullState`, `OnTickPatchState`

## Поведение

- **Master:** первый peer; failover — min `Ping()` ≥ 0, tie-break min `PeerID`; пустая комната → `master = nil`; `master == nil` при живых peers → переизбрание на ближайшем patch-тике или лениво при RPC→master
- **Election:** stale-кандидаты (`GetPeer` → `ErrPeerNotFound` — есть в state, нет в room) пропускаются, выборы идут среди живых; все stale → пустые выборы (`master = nil`, не ошибка); прочие ошибки `GetPeer` фейлят выборы. `assignMaster` validate-first: `GetPeer(masterID)` до записи `IsMaster`-флагов — state не рекламирует master, которого нет у policy (ADR 0005)
- **Masterless-эпизод:** `Error` при падении re-elect логируется один раз на эпизод (не на каждый тик), `Info` — при восстановлении master (tick re-elect, lazy RPC re-elect, join)
- **Join:** full state новому peer; patch остальным; `NickName` из `peer.GetNickName()`; stage-then-commit — при ошибке send `p.state`/`p.master` не мутируются (фантомного peer нет)
- **Ticks:** full state 4s, patch 50ms (дефолты); `0` в конструкторе → подстановка дефолтов
- **Master-only:** `FullEntities`, `PatchEntities`; input opcodes и RPC routing принимаются от всех peers (проверка членства в room state)
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
