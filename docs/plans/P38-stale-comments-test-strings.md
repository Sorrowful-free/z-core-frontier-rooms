# P38 — Stale comments + Russian test strings

## Why

Audit leftovers. (a) Comments translated to English but factually stale — they describe behavior that changed. (b) 24 Russian `t.Fatal`/`t.Fatalf` diagnostics left untranslated (were out of the comments-only scope).

## Scope (files allowed to edit)

Stale comments:

- `internal/usecase/room/disconnect_peer.go` — ~line 119: claims FIFO "remove patch → opcode". Post-P34 the `0x80` goes out immediately via `room.Send`; the remove patch follows on the next patch tick. Fix the wording.
- `internal/usecase/room/reconnect_room.go` — ~line 103: same stale ordering for "add patch → opcode" (`0x81` is immediate).
- `internal/adapter/transport/enet/connection_cgo.go` — ~lines 57, 99: "EventDisconnect → LeaveRoom" — the wired path is `DisconnectPeerUseCase` (grace), not direct LeaveRoom.
- `tests/adapter/realtime/policy/state/tick_bench_test.go` — ~line 24: "via the master's FullInput path" but the code sends `OpCodeFullEntities` — fix the comment.

Russian `t.Fatal`/`t.Fatalf` strings → English (format string text only, keep args):

- `tests/adapter/admission/admission_test.go` (2)
- `tests/adapter/realtime/policy/state/refresh_peer_pings_test.go` (2)
- `tests/adapter/realtime/policy/state/malformed_payload_test.go` (1)
- `tests/adapter/realtime/policy/state/send_failure_test.go` (1)
- `tests/adapter/realtime/policy/state/snapshot_isolation_test.go` (2)
- `tests/adapter/realtime/room_metrics_test.go` (2)
- `tests/adapter/realtime/room_leave_dedup_test.go` (1)
- `tests/adapter/realtime/peer_stop_test.go` (2)
- `tests/adapter/realtime/peer_drain_test.go` (6)
- `tests/adapter/reservation/reservation_metrics_test.go` (1)
- `tests/adapter/reservation/reservation_test.go` (1)
- `tests/domain/state/map_state_test.go` (1)
- `tests/usecase/room/delete_test.go` (1)
- `tests/integration/enet_shard_test.go` (1)

## Do

- Fix the four stale comments so the text matches current behavior (concise, same style).
- Translate the 24 test diagnostics to English; keep `%v`-args and parenthetical annotations.
- Do not change any code, identifiers, assertions, or test names.

## Do not touch

- `internal/delivery/enet/run_cgo.go` — the stale hostID comment there is owned by plan P37 (same file being edited).
- `invariants_test.go`/`master_recovery_test.go` — already fixed in the comments pass.
- Any non-listed file.

## Verification

```bash
go build ./...
go vet ./tests/...
./scripts/enet-verify.sh build   # connection_cgo.go touched
./scripts/enet-verify.sh vet
go test -race -shuffle=on -short -count=1 ./tests/...
grep -rnP '[\x{0400}-\x{04FF}]' --include='*.go' .   # expect: zero hits
```

Expected: zero Cyrillic anywhere in `.go`; suite green; enet-tagged build/vet pass.
