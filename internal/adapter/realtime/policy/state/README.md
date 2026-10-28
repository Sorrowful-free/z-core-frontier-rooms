# adapter/realtime/policy/state

`StateRoomPolicy` - authoritative state комнаты. Режим master-миграции —
глобальный флаг деплоя `StatePolicyConfig.AllowMasterMigration` (env
`STATE_POLICY_ALLOW_MASTER_MIGRATION`, P33): `false` — уход master'а
отвечает `domain.ErrRoomShouldClose` на входе `OnLeave` (до master-машины и
staged-мутаций; комната умирает, teardown — в usecase), уход non-master —
обычный путь. `StateRoomPolicyFactory` принимает конфиг конструктором и
вливает флаг полем (прецедент `p.master.metrics`).

## Интерфейс RoomPolicy

- `OnStart` / `OnStop`
- `OnJoin` / `OnLeave`
- `OnMessage` - opcodes entities/input/RPC (см. `client_state_room_policy.go`)
- `TickIntervals`, `OnTickFullState`, `OnTickPatchState`

## Поведение

- **Master-машина:** состояния `masterSet`/`masterless` (+ дедуп-флаг эпизода) в `master_machine.go`; триггеры (join/leave/tick-prune/RPC→master) эмитят `masterEvent`, `dispatch` решает переход - election, `IsMaster`-флаги и лог эпизода в одном месте
- **Master:** первый peer; failover - min `Ping()` ≥ 0, tie-break min `PeerID`; пустая комната → `master = nil`; `master == nil` при живых peers → переизбрание на ближайшем patch-тике или лениво при RPC→master
- **Election:** stale-кандидаты (`GetPeer` → `ErrPeerNotFound` - есть в state, нет в room) пропускаются, выборы идут среди живых; все stale → пустые выборы (`master = nil`, не ошибка); прочие ошибки `GetPeer` фейлят выборы. `assign` validate-first: `GetPeer(masterID)` до записи `IsMaster`-флагов - state не рекламирует master, которого нет у policy (ADR 0005)
- **Masterless-эпизод:** `Error` при падении re-elect логируется один раз на эпизод (не на каждый тик), `Info` - при восстановлении master (tick re-elect, lazy RPC re-elect, join)
- **Метрики (P19/P19b):** `rooms_master_elections_total` считает любое назначение master - и `assign` при перевыборах, и первый элекшн на join (`onPeerJoined`, эмит staged как и у `assign`: откат транзакции даёт редкий пересчёт); `StateRoomPolicyFactory` принимает `port/metrics.Metrics` (nil-safe) и вливает его в `masterMachine` полем
- **Join:** full state новому peer; patch остальным; `NickName` из `peer.GetNickName()`; stage-then-commit - при ошибке send `p.state`/`p.master` не мутируются (фантомного peer нет)
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
| `master_machine.go` | машина master-эпизодов: события триггеров, переходы, election |
| `state_room_policy_factory.go` | DI defaults |
| `state_policy_config.go` | `StatePolicyConfig` + `Validate()` (P33) |
