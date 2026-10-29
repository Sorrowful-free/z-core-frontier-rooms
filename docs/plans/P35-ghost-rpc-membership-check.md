# P35 — Ghost RPC membership check

## Why

Audit finding: `onClientRpc` relays `0x07` frames without checking that the sender is still a room member. Input handlers (`onClientFullInput`, `onClientPatchInput`) reject frames from peers absent from `state.Peers` with `ErrPeerNotFound` — the RPC path does not. A frame from an already-removed peer (queued before the leave delta lands) can be relayed to others — a "ghost input" (see GLOSSARY) the policy is supposed to reject.

## Scope (files allowed to edit)

- `internal/adapter/realtime/policy/state/client_state_room_policy.go`
- `tests/adapter/realtime/policy/state/` — one test file for the RPC ghost case (agent picks an existing RPC-appropriate file or adds `rpc_ghost_test.go`; external `package state_test` per convention)

## Do

1. In `onClientRpc` add a membership check mirroring the input handlers: decode the payload first (same order as inputs — malformed payload still answers `ErrInRoomInvalidPayload`), then

   ```go
   if _, ok := p.state.Peers[senderPeerID]; !ok {
       return fmt.Errorf("%w: %d", domain.ErrPeerNotFound, senderPeerID)
   }
   ```

   before computing the relay target. `ErrPeerNotFound` flows through `notifyInRoomError` → `InRoomErrorOpCode` (default `0x60`) — same surface the input ghost check produces; no new opcode.

2. Test: drive `policy.OnMessage` with a `RoomEvent{PeerID: <not-in-state>}` carrying a valid RPC frame — expect `ErrPeerNotFound` and no relayed `0x07` observed by other peers (use the existing harness/`testpeer` pattern from neighboring tests). Also confirm a legit peer's RPC still relays (existing tests likely cover — don't duplicate if so).

## Do not touch

- Other handlers, master machine, codecs, delivery layer.
- `docs/` — error opcode mapping is unchanged (already documented as generic in-room error); no doc edits needed.

## Verification

```bash
go build ./...
go test -race -count=1 ./tests/adapter/realtime/policy/state/...
go test -race -shuffle=on -short -count=1 ./tests/...
```

Expected: build clean, new test fails before the fix and passes after, suite green.
