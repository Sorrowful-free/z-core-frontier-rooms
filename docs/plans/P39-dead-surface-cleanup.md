# P39 — Dead surface cleanup

> Status: **approved — variant A (drop everything)**. Remove the dead helpers + their tests, prune unreachable HTTP mapper rows (sentinels stay — wire paths use them), delete the dead `hostRegistry == nil` branch, keep `0x72`/`0x7F` documented as reserved-not-emitted (already marked in `docs/client-protocol/errors.md`).

## Why

Audit found API/wire surface that is mapped/defined but unreachable in production today:

1. `internal/delivery/errors/` — exported helpers with **no production callers** (tests only): `SendReservationReject`, `ReservationRejectOpCode`, `SendInRoomError`, `IsJoinRejectOpCode`, `IsInRoomErrorOpCode`. In-room errors actually flow through `Room.Send`/`peer.Deliver`.
2. `internal/delivery/http/errors.go` — mapper rows unreachable via HTTP paths: `invalid_peer_id`, `room_already_exists`, `reservation_not_reserved`, `reservation_expired`, `reservation_already_exists`, `reservation_already_admitted`; `peer_already_in_room` is effectively unreachable (issue always allocates a fresh peerID).
3. Wire opcodes `0x72` (`OpReservationSlotHeld`) and `0x7F` (`OpReservationInternal`) — mapped but never emitted on wire (`0x72` fires only on the HTTP issue path → JSON; `0x7F`'s only emitter is the uncalled `SendReservationReject`). `docs/client-protocol/errors.md` marks them reserved.
4. `internal/usecase/room/issue_ticket.go` — `hostRegistry == nil` / `EnetPort = 0` branch is dead (composition root always passes a handler; `EnetConfig.Validate` requires ≥1 port).

## Scope (files allowed to edit)

- `internal/delivery/errors/join_reject.go`, `reservation_reject.go`
- `tests/delivery/errors/`
- `internal/delivery/http/errors.go` (+ mapper tests under `tests/delivery/http/`)
- `internal/usecase/room/issue_ticket.go` (+ `tests/usecase/room/issue_ticket_test.go` if the dead branch is covered)
- `internal/delivery/errors/README.md`, `internal/delivery/http/README.md` — remove references to deleted helpers/codes

## Do

- Remove `SendReservationReject`, `ReservationRejectOpCode`, `SendInRoomError`, `IsJoinRejectOpCode`, `IsInRoomErrorOpCode` and tests covering only them (keep `SendJoinReject`, `JoinRejectOpCode`, `IsJoinRejectOpCode` if actually called — verify callers first; keep `IsReservationRejectOpCode` if used internally by `JoinRejectOpCode`).
- Remove unreachable HTTP mapper rows: `invalid_peer_id`, `room_already_exists`, `reservation_not_reserved`, `reservation_expired`, `reservation_already_exists`, `reservation_already_admitted`, `peer_already_in_room`. Keep all domain sentinels — the wire join path maps them.
- Remove the `hostRegistry == nil` branch in `issue_ticket.go` (composition root always passes a handler) — `EnetPort` always assigned; keep the `omitempty` on the DTO as defensive JSON.
- Update the two READMEs where removed names appear.

## Do not touch

- `domain` sentinels, `internal/domain/opcodes.go` (0x72/0x7F stay defined = reserved wire surface), `docs/client-protocol/errors.md` (already marks them reserved), reservation adapter.

## Verification

```bash
go build ./... && go vet ./...
go test -race -shuffle=on -short -count=1 ./tests/...
grep -rn 'SendReservationReject\|ReservationRejectOpCode\|SendInRoomError\|IsJoinRejectOpCode\|IsInRoomErrorOpCode' --include='*.go' .  # expect zero
```
