# P36 — Tick intervals via env

## Why

Audit leftover: full/patch state tick intervals are compile-time constants (`DefaultFullStateInterval = 4s`, `DefaultPatchStateInterval = 50ms` in `state_room_policy.go`), hard-wired by `StateRoomPolicyFactory`. Operators can't tune emission cadence per deploy. `StatePolicyConfig` currently only carries `AllowMasterMigration`.

## Scope (files allowed to edit)

- `internal/adapter/realtime/policy/state/state_policy_config.go`
- `internal/adapter/realtime/policy/state/state_room_policy_factory.go`
- `internal/adapter/config/load.go`
- `internal/adapter/config/README.md`
- `internal/adapter/realtime/policy/state/README.md`
- `docs/client-protocol/state-sync.md`
- `tests/adapter/config/load_test.go` (add/adjust env cases)

## Do

1. `StatePolicyConfig` — add `FullStateInterval`, `PatchStateInterval time.Duration`; `Validate()` requires both `> 0` (they reach `NewStateRoomPolicy`/`normalizeTickInterval`; fail loudly at load instead of silently defaulting).
2. `load.go` — env `STATE_POLICY_FULL_STATE_INTERVAL`, `STATE_POLICY_PATCH_STATE_INTERVAL` via `durationFromEnv`, defaults `statepolicy.DefaultFullStateInterval` / `DefaultPatchStateInterval` (keep the constants as the defaults source).
3. Factory — pass `f.cfg.FullStateInterval` / `f.cfg.PatchStateInterval` into `NewStateRoomPolicy` instead of the constants.
4. Docs:
   - `internal/adapter/config/README.md` — two new env rows (name, default, meaning).
   - `internal/adapter/realtime/policy/state/README.md` — intervals now configurable via env.
   - `docs/client-protocol/state-sync.md` — replace the "(compile-time defaults — not env-tunable)" note with the env names + defaults.
5. Tests: `tests/adapter/config/load_test.go` — cases: unset → defaults (4s/50ms); set valid → parsed; set `0`/negative/garbage → error.

## Do not touch

- `cmd/rooms/main.go` — `cfg.StatePolicy` is already injected whole.
- `state_room_policy.go` constants/normalizeTickInterval (keep as defaults/fallback).
- Any other env or policy behavior (AllowMasterMigration stays as is).

## Verification

```bash
go build ./...
go test -race -count=1 ./tests/adapter/config/...
STATE_POLICY_FULL_STATE_INTERVAL=8s STATE_POLICY_PATCH_STATE_INTERVAL=100ms go test -count=1 -run TestLoad ./tests/adapter/config/...
go test -race -shuffle=on -short -count=1 ./tests/...
```

Expected: defaults preserved when env unset; valid durations parsed; invalid → `LoadFromEnv` error; suite green.
