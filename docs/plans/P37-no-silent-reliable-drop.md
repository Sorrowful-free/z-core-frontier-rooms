# P37 — No silent drop of reliable frames

## Why

Audit leftover: three queue-overflow paths silently drop frames including reliable ones — violating the "reliable ⇒ delivered" contract a client reasonably assumes:

1. `peer.tryDeliver` (`peer.go`) — `peer.outbound` full → drop + `PeerOutboundDropped` + `ErrQueueFull`; `room.Send` just skips to the next peer. Server→client is always reliable in practice — a peer's replica silently desyncs.
2. `processIncomingEvents` (`peer.go`) — `room.incoming` (sink.Deliver → `ErrQueueFull`) → drop + warn + continue. Reliable client frames vanish.
3. ENet session incoming queue (`delivery/enet/run_cgo.go`) — full → drop + `EnetIncomingDropped` + warn; connection survives.

Decision (user): **a reliable frame is never silently dropped — on overflow the transport escalates to teardown** (a stalled/flooded peer is a zombie anyway; graceful teardown beats silent half-delivery). Frames marked `DeliveryUnreliable` may still be dropped.

## Scope (files allowed to edit)

- `internal/adapter/realtime/peer.go`
- `internal/adapter/realtime/room.go` (only if needed — Deliver already returns `ErrQueueFull`; the broadcast-skip may stay since the peer is being torn down anyway)
- `internal/delivery/enet/run_cgo.go`
- `internal/delivery/enet/session_cgo.go` (only if needed)
- `internal/adapter/realtime/README.md`, `internal/delivery/enet/README.md`, `internal/adapter/transport/README.md`
- `docs/client-protocol/enet.md` (the "queue overflow → drop, connection survives" line)
- `internal/port/realtime/` doc comments on `Peer.Deliver`/`Send` if the contract wording needs the new guarantee
- `tests/adapter/realtime/` and `tests/delivery/enet/` — extend/add tests

## Do

1. **`tryDeliver`**: on full queue inspect `ev.Frame.Delivery`:
   - `DeliveryUnreliable` → keep current drop + warn + `PeerOutboundDropped` + `ErrQueueFull`.
   - reliable/`DeliveryDefault`/`DeliveryReliable` → warn "reliable frame undeliverable, closing connection" + `_ = p.connection.Close()` + return `ErrQueueFull`. The dead conn surfaces via the normal teardown path (write-loop `sendFrame` error / read error → `DisconnectPeer` → grace). Do NOT introduce blocking sends.
2. **`processIncomingEvents`**: `ErrQueueFull` + frame with `Delivery != DeliveryUnreliable` → treat like the existing fatal branch: `_ = peer.connection.Close()` and return (read loop exits → transport teardown path). Unreliable → keep drop + warn + `continue`.
3. **ENet session incoming** (`run_cgo.go`): overflow on a reliable frame → `disconnectSession` (graceful, reuses existing path → `DisconnectPeer` when admitted) instead of drop; unreliable → keep drop + metric + warn.
4. **Stale comment in the same file** (from audit): `run_cgo.go` ~line 185 "A ticket without hostID passes on any host" — wrong (unbound `hostID=0` on ENet admit → `ErrWrongHost`); fix the comment while editing the file.
5. Metrics: keep the existing drop counters for unreliable drops; reliable escalation needs no new metric (warn + normal disconnect path).
6. Docs: update the claims that a queue overflow never breaks the connection — now: unreliable frames may be dropped; a reliable frame on a full queue tears the connection down (→ disconnect, then grace). Update `enet.md`, `internal/delivery/enet/README.md`, `internal/adapter/realtime/README.md`, `internal/adapter/transport/README.md` accordingly.
7. Tests: outbound full + reliable → connection closed / peer torn down; outbound full + unreliable → drop, connection alive; room.incoming full + reliable → teardown; (enet) session queue full + reliable → disconnect, + unreliable → drop. Use existing test peers/harness.

## Do not touch

- `connection_cgo.go`, `flags_cgo.go`, `hostloop_*`, WS `connection.go` internals, codecs, policy, usecase logic beyond the listed files.
- Queue sizes/env names/defaults.

## Verification

```bash
go build ./...
go vet ./...
go test -race -count=1 ./tests/adapter/realtime/...
./scripts/enet-verify.sh build   # *_cgo.go touched
./scripts/enet-verify.sh vet
./scripts/enet-verify.sh test    # if tagged tests exist/changed
go test -race -shuffle=on -short -count=1 ./tests/...
```

Expected: no `nil`-returned reliable drop anywhere; suite green incl. new overflow tests; enet-tagged build/vet pass.
