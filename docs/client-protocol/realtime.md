# Realtime data plane

Shared data-plane contract after the [HTTP ticket](http.md).

| Transport | Purpose | Document |
|-----------|---------|----------|
| **ENet UDP** | Primary (Godot) | **[enet.md](enet.md)** |
| WebSocket | Quick debug / smoke test | below |

Ticket format, frame format, and the post-join game loop are **identical**; only the way the ticket is handed over at connect differs.

---

## Join ticket (kind `1`, binary)

The client **never generates** a ticket — it only receives one over HTTP. The first byte of a credential is its `kind`: `1` = join ticket, `2` = reconnect token.

```text
kind        u8   = 1
room_id     u64 BE
peer_id     u64 BE
issued_at   u64 BE
expires_at  u64 BE
host_id     u16 BE  (0 = unbound; on ENet admit → reject 0x77)
nick_len    u8
nick        UTF-8 bytes (1…64)
hmac        32 bytes SHA-256 HMAC
```

Size: `68 + len(nick)` … `132` bytes.

| Transport | How the ticket is passed |
|-----------|--------------------------|
| ENet | base64url-decode → **raw bytes** in the first UDP packet |
| WebSocket | base64url string in `?token=` (server decodes) |

More on ENet: [enet.md](enet.md). Admission: [internal/adapter/admission/README.md](../../internal/adapter/admission/README.md).

---

## Data-plane frame (shared)

After a successful join (WS or ENet):

```text
[OpCode: u8][Payload: variable]
```

| OpCode | Purpose |
|--------|---------|
| `0x01`–`0x07` | State / input / RPC — [wire-state-codec.md](../wire-state-codec.md), [state-sync.md](state-sync.md) |
| `0x40`–`0x5F` | Join reject — [errors.md](errors.md) |
| `0x60`–`0x6F` | In-room error (connection stays open) |
| `0x70`–`0x7F` | Reservation reject (at join) |
| `0x80`–`0x82` | Room notifications `OpPeerDisconnected`/`OpPeerReconnected` (payload `peerID u64 BE`), `OpRoomClosed` `0x82` (room closed by the server: host left in a no-migration deploy, `DELETE /rooms/:id`, or shutdown; empty payload, disconnect follows) — [errors.md](errors.md#room-notifications-0x80-not-errors) |

**Limit:** `TRANSPORT_MAX_INCOMING_FRAME_BYTES` (env, default 262144).

### Post-join loop

1. `0x01` FullState — initialize the local state.
2. `0x02` PatchState — merge patches.
3. `0x05`/`0x06` input — any peer.
4. `0x03`/`0x04` entities — master only.
5. `0x07` RPC — relayed by target.

---

## Reconnect (P20)

A short transport break does **not** destroy identity: the peer leaves the room immediately (others get a remove patch and `0x80`), but the reservation slot keeps capacity and peerID within the `RESERVATION_RECONNECT_TTL` grace window (server-side, default `30s`, counted from the disconnect).

1. `POST /rooms/:id/tickets` returns a `reconnect_token` — keep it for the disconnect case (the token has no expiry; it lives while the admitted slot lives).
2. Disconnect → the server broadcasts `0x80` to the rest.
3. Re-admit over the same transport: WS `?token=<reconnect_token>` or the first ENet packet — instead of the join ticket.
4. Success: the peer returns under the **same peerID** with the same nick (nick comes from the slot — the token carries none) and gets `0x01` FullState like a regular join; others get `0x81` with `peerID u64 BE`.
5. Rejection — the usual join reject (map: [errors.md](errors.md#resume-reject-reconnect_token)): `0x41` corrupt/foreign token, `0x43` room deleted, `0x73` no slot / not admitted, `0x74` grace expired, `0x77` token `host_id` ≠ accepting host.
6. Newest connection wins: a reconnect while the old conn is alive evicts it (Replace).

- **`0x81` without a preceding `0x80` is normal** when Replace evicts a live peer (double resume with one token / connection takeover): the old conn's teardown goes through an identity-check teardown without an `0x80` broadcast, others see remove+add patches and `0x81`. The client handles `0x81` idempotently — the peer becomes live regardless of the previous disconnected flag.
- **The grace deadline is enforced at resume (P24):** resuming past the window (peer not in the room and `lastSeen + RESERVATION_RECONNECT_TTL` elapsed) is rejected with `0x74` `OpReservationExpired` immediately, without waiting for the sweep; the deadline is checked **before** the `lastSeen` refresh — rejected attempts do not extend grace. With a live peer the deadline does not apply — Replace (item 6).

**Reconnect token (kind `2`):** `kind(1)=2 | room_id u64 | peer_id u64 | host_id u16 | HMAC-SHA256(32)` — 51 bytes, base64url like the join ticket; no nick and no timestamps; `host_id` inherits the original ticket binding (strict ENet binding).

---

## WebSocket (debug)

Use WS to verify HTTP → ticket → join → state **without** ENet/CGO.

| Parameter | Value |
|-----------|-------|
| URL | `ws://<host>/ws?token=<base64url>` |
| Token | The string from the `POST /tickets` JSON as-is, `uri_encode`d into the URL |
| Messages | Binary WS frames only |
| API key | Not required (registered outside the auth group) |

| Join outcome | Behavior |
|--------------|----------|
| No token | `0x40` OpEmptyToken → close |
| Corrupt base64url token | `0x41` OpInvalidToken → close |
| Token over the limit | Close without a frame (like an oversize ENet packet) |
| Join error | 1 byte `0x40+` → close |
| Success | First frame `0x01` FullState |

WS has no reliable/unreliable distinction — everything is implicit reliable. For a production client see [enet.md](enet.md).

### Godot: minimal smoke test

```gdscript
var ws := WebSocketPeer.new()
ws.connect_to_url("ws://localhost:3000/ws?token=%s" % token.uri_encode())

func _process(_delta):
    ws.poll()
    while ws.get_available_packet_count() > 0:
        var packet := ws.get_packet()
        _handle_frame(packet[0], packet.slice(1))

func send_frame(opcode: int, payload: PackedByteArray) -> void:
    var frame := PackedByteArray([opcode])
    frame.append_array(payload)
    ws.send(frame)
```

WS ping/pong is the transport RTT used by `Peer.Ping()` on the server; it is not part of the game protocol. A pong also confirms liveness: if neither frames nor pongs arrive for longer than `TRANSPORT_PEER_IDLE_TIMEOUT` (default 30s), the server silently closes the connection (idle eviction, no opcode).

---

## Next

- **ENet (primary):** [enet.md](enet.md)
- State sync: [state-sync.md](state-sync.md)
- Errors: [errors.md](errors.md)
