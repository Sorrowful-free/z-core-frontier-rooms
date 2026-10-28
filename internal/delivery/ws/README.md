# delivery/ws

WebSocket data plane on the same `*fiber.App` as HTTP.

## Endpoint

`GET /ws?token=<base64url>`

The ticket is still in the query (client contract). A **base64url** string from `POST /rooms/:id/tickets` (no padding), URL-encoded in the query — a join ticket or a `reconnect_token` (kind `2`, P20) for resuming under the same peerID. The server decodes it into raw bytes before `AdmitPeerUseCase`. The Fiber access log (`fibzap`) writes `path` without the query so the token does not end up in logs.

Client spec: [docs/client-protocol/realtime.md](../../../docs/client-protocol/realtime.md) (overview), [docs/client-protocol/enet.md](../../../docs/client-protocol/enet.md) (primary transport).

1. Decode base64url from the query → raw credential bytes (join ticket / reconnect token)
2. `AdmitPeerUseCase.Admit(..., HostIDUnbound)` — the facade validates the token once and routes join/resume by `claims.Kind` (P20)
3. `connection.Receive` loop → `room.Deliver`
4. On disconnect → `DisconnectPeer` with the `PeerSession` from admit (P20: `MarkDisconnected` → `Leave` → `Stop` → `OpPeerDisconnected`; grace window `RESERVATION_RECONNECT_TTL`; in a no-migration deployment host disconnect → `ErrRoomShouldClose` → teardown: survivors get `0x82 OpRoomClosed` + close, P33)

Join error: one OpCode frame (`delivery/errors.SendJoinReject`) → close WS.

No `token` in the query — reject frame `OpEmptyToken` (`0x40`) → close; broken base64url — `OpInvalidToken` (`0x41`); token longer than `MaxIncomingFrameBytes` — close **without** a frame (parity with the ENet oversize packet).

Every **successfully sent** reject frame is counted by the `rooms_join_rejects_total{opcode}` metric (P19b: `SendJoinReject` returns the send error; `sendJoinReject` emits `JoinRejected(op)` only on nil, an error is Warn-logged).

Idle peers are evicted by a sweep (`EvictIdlePeersUseCase`) when the connection stops proving liveness: incoming frames and pongs advance `lastSeen`, `Send` does not; a dead transport → `IsAlive() = false` → `DisconnectPeer` (ADR 0001; P20 — a zombie gets the same grace window as a clean disconnect).

Error import: `deliveryerrors ".../delivery/errors"`.
