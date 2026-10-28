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
- **Эмиссия state (P34, tick-coalesced):** единственный регулярный эмиттер `OpCodePatchState` — `OnTickPatchState`. `OnMessage`-обработчики и join/leave мутируют `p.state` точечно (COW-записи, без clone/diff/send), дельта доезжает ближайшим тиком универсальным broadcast всем без исключений — publisher получает эхо своих дельт (client-side apply идемпотентен, `ApplyMapStatePatch` мягкий). Immediate одно: full state новоприбывшему peer на join. `prevState` — shallow `Snapshot()` (COW-инвариант `RoomState`), коммитится только после успешного send — сорванная рассылка не теряет дельту
- **Join:** full state новому peer напрямую (peer ещё вне `r.peers`); запись в `p.state` коммитится после успешной доставки из encode-only shallow-view — при ошибке send `p.state`/`p.master` не мутируются (фантомного peer нет); join-дельта остальным — patch-тиком
- **Ticks:** full state 4s, patch 50ms (дефолты); `0` в конструкторе → подстановка дефолтов. Сплит бывшего `refreshPeerPings` (P34): ping пишется в state на **full-тике** до `sendFullState` (каденция ping = fullStateInterval, patch-diff его не видит), stale-scan + `evTickPrune` — на **patch-тике** (`pruneStalePeers`, masterless recovery держит 50мс)
- **Master-only:** `FullEntities`, `PatchEntities`; input opcodes и RPC routing принимаются от всех peers (проверка членства в room state)
- **Patch apply (сервер):** двухфазный — staged-результаты по ключам патча (clone только затронутых), запись разом; `Updated` на отсутствующем ключе — `ErrInRoomInvalidPayload`
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
