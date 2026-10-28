# Tests

All project tests live here, not next to the sources in `internal/` and `cmd/`.

The structure mirrors the application layers:

```text
tests/
  mocks/                — mockgen from internal/port (gomock), go generate ./tests/mocks/...
  delivery/errors/      — join_reject, reservation_reject, in_room, wire helpers
  adapter/config/       — LoadFromEnv, admission + http auth + transport + enet env
  adapter/httpauth/     — HTTPAuthConfig.Validate
  adapter/httplimits/   — HTTPLimitsConfig, Limiter
  adapter/admission/    — ticket Issue/Validate, AdmissionConfig
  adapter/reservation/  — slots: Reserve / Admit / Revoke / State
  adapter/identity/     — Counter: AllocateRoomID / AllocatePeerID, wrap skip 0
  adapter/realtime/     — Room Join/Replace, policy without deadlock, peer factory nick
  adapter/realtime/policy/state/ — StateRoomPolicy, master election, tick intervals
  adapter/realtime/codec/ — round-trip state/input/rpc/room wire codec
  domain/               — ValidateNickName, DecodeIncomingFrame
  domain/state/         — atomic apply patch (entity/component)
  adapter/registry/     — RoomRegistry, Shutdown
  adapter/metrics/      — prometheus Metrics: counters/gauges via Gatherer, MetricsConfig.Validate
  delivery/admin/       — GET /metrics of the admin listener: 200 + exposition text (app.Test)
  usecase/room/         — Create, Delete, GetList, IssueTicket, JoinRoom, LeaveRoom (gomock)
  testutil/peer/        — parameterizable realtime.Peer: ping/alive/deliverErr + PeerEvent capture
  testutil/env/         — shared composition root: adapters + use cases + HTTP/WS routes on fiber.App
                          (defaults must mirror prod transport config: zero values mask delivery errors — see the ErrQueueFull case)
  delivery/http/        — REST JSON (app.Test on top of testutil/env)
  delivery/ws/          — WS join lifecycle (real listener + fasthttp/websocket)
  integration/          — end-to-end scenarios: room lifecycle create→issue→2×WS join→disconnect→leave;
                          reconnect (P20): WS disconnect → 0x80 → resume → 0x81 + resync;
                          ENet (tag enet): basic reconnect, grace-expiry 0x74 (P24),
                          strict host binding 0x77;
                          e2e live match (-short skip): shared driver + WS and ENet clients —
                          WS (~60s): 4 clients, host dropout → idle evict → 2 master migrations;
                          ENet (~115s, tag enet): same, dropout = libenet silence → peer timeout;
                          mixed (~115s, tag enet): host on ENet, slaves WS+ENet, cross-transport
```

Packages are **external** (`package *_test`), importing the module `github.com/Sorrowful-free/z-core-frontier-rooms/...`.

Run from the module root:

```bash
go test ./tests/...
```

With race detector (recommended for realtime):

```bash
go test -race ./tests/...
```

ENet (cgo, tag `enet`) — compile smoke for the `go-enet` packages:

```bash
# Linux/macOS / MSYS2: gcc and CGO_ENABLED=1 are required
CGO_ENABLED=1 go test -tags enet ./tests/delivery/enet/...
```

Full test run with the same tag (rebuilds dependencies under `enet`):

```bash
CGO_ENABLED=1 go test -tags enet ./tests/...
```

Windows (PowerShell, MSYS2 gcc not in the session PATH):

```powershell
$env:Path = "C:\msys64\mingw64\bin;" + $env:Path
$env:CGO_ENABLED = "1"
go test -tags enet ./tests/delivery/enet/...
go test -tags enet ./tests/...
```

Without `-tags enet`, `tests/delivery/enet` has a single skip test with a hint.

## Benchmarks

`*_bench_test.go` files — performance tests of hot paths (codec, patch, policy tick, room loop). Production code is not changed.

Run all benchmarks:

```bash
go test -bench=Benchmark -benchmem -count=1 ./tests/...
```

Selectively:

```bash
go test -bench=BenchmarkDecodeIncomingFrame -benchmem -count=1 ./tests/domain/...
go test -bench=BenchmarkRoomState_ -benchmem -count=1 ./tests/domain/state/...
go test -bench=BenchmarkRoomStateCodec_ -benchmem -count=1 ./tests/adapter/realtime/codec/...
go test -bench=BenchmarkOnTickPatchState -benchmem -count=1 ./tests/adapter/realtime/policy/state/...
go test -bench=BenchmarkRoom_ -benchmem -count=1 ./tests/adapter/realtime/...
go test -bench=BenchmarkReservation -benchmem -count=1 ./tests/adapter/reservation/...
```

Before/after comparison (locally): `go install golang.org/x/perf/cmd/benchstat@latest`, then `-count=10` and `benchstat`.

### Tiers

| Tier | Package | What is measured |
|------|---------|------------------|
| A — CPU | `tests/domain`, `tests/domain/state`, `tests/adapter/realtime/codec` | `DecodeIncomingFrame`, `MakePatch`/`ApplyPatch`/`Clone`, wire encode/decode |
| B — policy | `tests/adapter/realtime/policy/state` | `OnTickPatchState`, `OnMessage` PatchInput (no network) |
| C — room | `tests/adapter/realtime` | `Room.Send` broadcast, `Deliver`, `Join` |
| D — reservation | `tests/adapter/reservation` | `Reserve` / issue-join path; parallel spread |

Room fixtures with N peers: [`tests/testutil/bench/room_state.go`](testutil/bench/room_state.go) (`BuildRoomState`).

**Out of scope:** CI regression (benchstat baseline), load soak tests, ENet/WS e2e perf.

## Fuzzing

`tests/adapter/realtime/codec/fuzz_test.go` — fuzzing of all wire codec decoders. Invariants: decode does not panic, decoding the same bytes again is deterministic, `Encode` on a decoded value does not fail (byte-exact roundtrip is not checked — variable-length fields).

`-fuzz` matches exactly one `Fuzz*` target per run:

```bash
go test -fuzz=FuzzRoomStateDecode -fuzztime=30s ./tests/adapter/realtime/codec/
```

Targets: `FuzzRoomStateDecode`, `FuzzRoomStateDecodePatch`, `FuzzEntitiesStateDecode`, `FuzzEntitiesStateDecodePatch`, `FuzzInputStateDecode`, `FuzzInputStateDecodePatch`, `FuzzRpcStateDecode`.

In CI — the `fuzz` job in `.github/workflows/test.yml` (30s per target); crashers are saved as an artifact from `tests/adapter/realtime/codec/testdata/fuzz/`.

## Per-package coverage

The scenario inventory is deliberately not duplicated: it goes stale with every
test change. What a package covers — see its `*_test.go`; navigation is via
the directory tree above (it mirrors the application layers).

After changing `internal/port/*` interfaces:

```bash
go generate ./tests/mocks/...
```

See [tests/mocks/README.md](mocks/README.md).

## E2E live match

Shared client layer and scenario driver — `match_e2e_client_test.go` (`matchClientBase` + `matchClient` + `runLiveMatch`); transports: `wsClient` (`match_e2e_test.go`) and `enetClient` (`match_e2e_enet_test.go`, tag `enet && cgo`). All scenarios are skipped under `-short`.

`TestE2E_LiveMatch_HostMigration` (plan `docs/plans/P11-e2e-match.md`) — 4 WS clients, ~60s:

```bash
go test -v -race -run '^TestE2E_LiveMatch_HostMigration$' -count=1 -timeout 120s ./tests/integration/
```

`TestE2E_LiveMatch_ENet` — 4 ENet clients, ~115s: host dropout — honest libenet silence (the client stops calling `host.Service`, the UDP socket stays alive) → server-side peer timeout (measured ~10s on localhost, bound 70s) → `EventDisconnect` → `LeaveRoom` → migration. `TestE2E_LiveMatch_Mixed` — host on ENet, slaves 1×ENet + 2×WS: cross-transport entities/input/migrations. Locally without libenet-dev — via Docker:

```bash
./scripts/enet-verify.sh test   # delivery/enet + tagged e2e in the zcore-rooms-dev image
# or with libenet-dev installed:
CGO_ENABLED=1 go test -v -race -tags enet -run 'TestE2E' -count=1 -timeout 300s ./tests/integration/
```

Each client (`host`, `s1..s3`/`e1..e3`/`w1..w2`) — pump + send-loop + stats-loop goroutines; `t.Logf` logs with a nickname prefix form a single chronological feed under `-v` and on failure: `joined as peer N`, `master is now ...`, `became/lost master`, `first entity snapshot from owner ...`, `observed input of ...`, `my input observed`, `in-room error frame`, `disconnected`. Scenario markers — `[scenario] ...` (host goes silent, master leaves, `waitFor` steps — ok).

## Next priority

Queue is empty — the first integration scenario (`tests/integration`, ADR-0002) is done.

For a new join/reservation error — add a case in `join_reject_test.go` / `reservation_reject_test.go` (see [.cursor/rules/join-error-opcodes.mdc](../.cursor/rules/join-error-opcodes.mdc)).

Details — index [README.md](../README.md), architecture [docs/architecture.md](../docs/architecture.md), `internal/**/README.md` packages.
