# adapter/realtime/policy/state

`StateRoomPolicy` — the room's authoritative state. Master-migration mode is
a global deploy flag `StatePolicyConfig.AllowMasterMigration` (env
`STATE_POLICY_ALLOW_MASTER_MIGRATION`, P33): `false` — a master leave answers
`domain.ErrRoomShouldClose` at the `OnLeave` entry (before the master machine
and staged mutations; the room dies, teardown in the usecase), a non-master
leave takes the normal path. `StateRoomPolicyFactory` accepts the config in
its constructor and injects the flag as a field (precedent `p.master.metrics`).

## RoomPolicy interface

- `OnStart` / `OnStop`
- `OnJoin` / `OnLeave`
- `OnMessage` — entities/input/RPC opcodes (see `client_state_room_policy.go`)
- `TickIntervals`, `OnTickFullState`, `OnTickPatchState`

## Behavior

- **Master machine:** states `masterSet`/`masterless` (+ an episode dedup flag) in `master_machine.go`; triggers (join/leave/tick-prune/RPC→master) emit `masterEvent`, `dispatch` resolves the transition — election, `IsMaster` flags and the episode log in one place
- **Master:** the first peer; failover — min `Ping()` ≥ 0, tie-break min `PeerID`; empty room → `master = nil`; `master == nil` with live peers → re-election on the nearest patch tick or lazily on RPC→master
- **Election:** stale candidates (`GetPeer` → `ErrPeerNotFound` — present in state, absent from the room) are skipped, the election runs among live peers; all stale → empty election (`master = nil`, not an error); other `GetPeer` errors fail the election. `assign` is validate-first: `GetPeer(masterID)` before writing `IsMaster` flags — state never advertises a master the policy does not have (ADR 0005)
- **Masterless episode:** an `Error` on re-elect failure is logged once per episode (not on every tick), `Info` — when the master is restored (tick re-elect, lazy RPC re-elect, join)
- **Metrics (P19/P19b):** `rooms_master_elections_total` counts every master assignment — both `assign` during re-elections and the first election on join (`onPeerJoined`, emitted staged like `assign`: a transaction rollback causes a rare overcount); `StateRoomPolicyFactory` accepts `port/metrics.Metrics` (nil-safe) and injects it into `masterMachine` as a field
- **State emission (P34, tick-coalesced):** the only regular emitter of `OpCodePatchState` is `OnTickPatchState`. `OnMessage` handlers and join/leave mutate `p.state` pointwise (COW writes, no clone/diff/send); the delta arrives on the nearest tick via an unconditional broadcast to everyone — the publisher receives an echo of its own deltas (client-side apply is idempotent, `ApplyMapStatePatch` is lenient). The only immediate send: full state to the newly arrived peer on join. `prevState` is a shallow `Snapshot()` (the `RoomState` COW invariant), committed only after a successful send — a failed broadcast does not lose the delta
- **Join:** full state to the new peer directly (the peer is not yet in `r.peers`); the `p.state` write is committed after successful delivery from an encode-only shallow view — on a send error `p.state`/`p.master` are not mutated (no phantom peer); the join delta to the others goes out on the patch tick
- **Ticks:** full state 4s, patch 50ms (defaults); `0` in the constructor → defaults substituted. Split of the former `refreshPeerPings` (P34): the ping is written to state on the **full tick** before `sendFullState` (ping cadence = fullStateInterval, the patch diff does not see it); stale scan + `evTickPrune` — on the **patch tick** (`pruneStalePeers`, masterless recovery keeps 50ms)
- **Master-only:** `FullEntities`, `PatchEntities`; input opcodes and RPC routing are accepted from all peers (membership in room state is checked)
- **Patch apply (server):** two-phase — staged results per patch key (clone only the touched ones), written in one go; `Updated` on a missing key — `ErrInRoomInvalidPayload`
- **Errors:** `domain` sentinels → in-room OpCode to the sender

## Files

| File | Role |
|------|------|
| `state_room_policy.go` | lifecycle, opcode routing |
| `server_state_room_policy.go` | join, broadcast, pings |
| `client_state_room_policy.go` | master messages |
| `master_machine.go` | master-episode machine: trigger events, transitions, election |
| `state_room_policy_factory.go` | DI defaults |
| `state_policy_config.go` | `StatePolicyConfig` + `Validate()` (P33) |
