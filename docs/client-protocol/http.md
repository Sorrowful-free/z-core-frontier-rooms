# HTTP control plane

REST JSON on the same host as WebSocket. Default bind: `:3000` (`HTTP_ADDR`, all interfaces).

Data plane: **ENet UDP** (primary) or WebSocket (debug) — join only with a ticket.

## Authentication

One of the headers (unless `HTTP_AUTH_DISABLED` is `true`):

```http
Authorization: Bearer <HTTP_API_KEY>
```

or

```http
X-API-Key: <HTTP_API_KEY>
```

Missing or wrong key → `401`:

```json
{"code":"unauthorized","message":"..."}
```

Auth covers only the REST endpoints below. `GET /ws` (the WebSocket join entry) is registered outside the auth group — it requires **no API key** and is not rate-limited.

## Endpoints

### `POST /rooms` — create a room and issue the host ticket

**Request:**

```json
{
  "capacity": 8,
  "nick_name": "HostPlayer",
  "password": "optional-secret",
  "attributes": {
    "map": "de_dust2",
    "mode": "deathmatch"
  }
}
```

| Field | Type | Rules |
|-------|------|-------|
| `capacity` | int | Required, `1…127` (`domain.MaxRoomCapacity`) |
| `nick_name` | string | Required; see [nick_name rules](#nick_name-rules) |
| `password` | string | Optional; when set — required on join via `/tickets` and on delete |
| `attributes` | object | Optional; arbitrary JSON dictionary (map name, mode, etc.); returned by create/list |

The room ID is assigned by the server — the request has no `id` field.

**Success `201`:**

```json
{
  "token": "AQAAAA...base64url...",
  "reconnect_token": "AgAAAA...base64url...",
  "enet_port": 7777,
  "room": {
    "id": 42,
    "attributes": {
      "map": "de_dust2",
      "mode": "deathmatch"
    },
    "peers": []
  }
}
```

`token` — **base64.RawURLEncoding** (no `=` padding). Decode into the raw join-ticket bytes (kind `1`) → first UDP packet ([enet.md](enet.md)) or the `?token=` query for WS.

`reconnect_token` — **base64.RawURLEncoding**, reconnect token (kind `2`, P20): resume under the same peerID after a disconnect within the `RESERVATION_RECONNECT_TTL` grace window; goes to the same admit entry (`?token=` / first ENet packet). See [realtime.md](realtime.md#reconnect-p20).

`enet_port` — assigned UDP port of the ENet host (P18, ticket-to-host binding). In practice the field is **always present**: the server requires at least one port in `ENET_LISTEN_PORTS` at startup and always assigns a host. Note the port reflects configured hosts — in a binary built without `-tags enet` nothing actually listens on it.

**Errors:** `400` `invalid_capacity`, `invalid_nick_name`, `invalid_request`; `503` `rooms_limit_reached`; `429` `rate_limited`; plus issue-ticket errors (`401` `invalid_credentials`, `409` `reservation_full`, `ticket_slot_held`, etc.) — on an issue failure the created room is rolled back.

---

### `GET /rooms` — room list

**Success `200`:**

```json
{
  "rooms": [
    {
      "id": 42,
      "attributes": {
        "map": "de_dust2"
      },
      "peers": [
        {"peer_id": 1, "nick_name": "Alice", "ping": 12}
      ]
    }
  ]
}
```

`ping` is in **milliseconds** (ENet RTT).

**Errors:** `429` `rate_limited`.

---

### `DELETE /rooms/:id` — delete a room

**Request** (if the room has a password):

```json
{"password": "optional-secret"}
```

**Success:** `204` with no body.

**Behavior for connected peers (P31):** evicted peers receive `0x82 OpRoomClosed` (empty payload) before the connection is torn down — the same frame as a room death on host leave in a no-migration deploy; client handling is identical (the room no longer exists, see [errors.md](errors.md#room-notifications-0x80-not-errors)).

**Errors:** `400` `invalid_room_id` (invalid `:id`), `404` `room_not_found`, `401` `invalid_credentials`, `429` `rate_limited`.

---

### `POST /rooms/:id/tickets` — issue a ticket (joiners)

For players joining an **existing** room. The host gets a ticket on create.

**Request:**

```json
{
  "nick_name": "Player1",
  "password": ""
}
```

| Field | Type | Rules |
|-------|------|-------|
| `nick_name` | string | Required; see below |
| `password` | string | Room password (empty string if the room has none) |

**Success `201`:**

```json
{
  "token": "AQAAAA...base64url...",
  "reconnect_token": "AgAAAA...base64url...",
  "enet_port": 7778,
  "room": {
    "id": 42,
    "attributes": {
      "map": "de_dust2"
    },
    "peers": []
  }
}
```

`room` — the same DTO as in create/list: `id`, `attributes?`, `peers[]` (peers currently **in** the room, excluding the just-reserved slot).

`token` — **base64.RawURLEncoding** (no `=` padding). `reconnect_token` — reconnect token (kind `2`, P20) for resuming under the same peerID after a disconnect ([realtime.md](realtime.md#reconnect-p20)).

- **ENet (primary):** decode into raw join-ticket bytes → first UDP packet to port `enet_port` ([enet.md](enet.md)). The ticket is bound to the assigned host: admitting on a different port → reject `0x77`.
- **WebSocket (debug):** the string as-is in the `?token=` query ([realtime.md](realtime.md#websocket-debug)).

**Errors:**

| HTTP | `code` | When |
|------|--------|------|
| `400` | `invalid_nick_name` | Empty nick, control chars, > 64 UTF-8 bytes |
| `400` | `invalid_request` | Malformed JSON body |
| `400` | `invalid_room_id` | Invalid `:id` in path |
| `401` | `invalid_credentials` | Wrong room password |
| `404` | `room_not_found` | No such room |
| `409` | `ticket_slot_held` | A `reserved` slot with the **same** `nick_name` already exists (TTL not expired) |
| `409` | `reservation_full` | No free slots |
| `429` | `rate_limited` | REST endpoint limit from this IP |

Generic mapper codes that can also surface on edge races: `404` `reservation_not_found`, `500` `internal_error`.

### `POST /rooms/:id/leave` — self-leave (P32)

Body: `{"reconnect_token": "<base64url>"}` — the peer's reconnect token (kind=2 claims prove `peerID`+`roomID`; `roomID` must match `:id`). Invalid/foreign/join token → `401 invalid_credentials`. Idempotent: peer already out of the room → `204`. The peer's connection is closed by the server (drain-aware stop). Kicking other players over HTTP does not exist.

**Errors:** `400` `invalid_room_id`, `400` `invalid_request`, `401` `invalid_credentials`, `429` `rate_limited` (shares the **delete** bucket `HTTP_DELETE_RATE_PER_MIN`).

## `nick_name` rules

- Non-empty UTF-8 string
- Length **in bytes** ≤ 64
- No control characters (`codepoint < 0x20`)

## Reservation slots (short)

Issuing a ticket reserves a slot for a TTL (`ADMISSION_TTL`). While a slot is `reserved` with the same `nick_name`, a repeat issue returns `ticket_slot_held`; duplicate nicks in the room are otherwise allowed — issue always allocates a fresh `peer_id`. Details: [internal/adapter/reservation/README.md](../../internal/adapter/reservation/README.md).

## Godot: create room example (host)

```gdscript
var http := HTTPRequest.new()
add_child(http)
var headers := PackedStringArray(["Authorization: Bearer " + api_key, "Content-Type: application/json"])
var body := JSON.stringify({
	"capacity": 8,
	"nick_name": "HostPlayer",
	"password": "",
	"attributes": {"map": "de_dust2"}
})
http.request("http://localhost:3000/rooms", headers, HTTPClient.METHOD_POST, body)
# Response JSON: var token: String = parsed["token"], var room_id: int = parsed["room"]["id"]
```

## Godot: issue ticket example (joiner)

```gdscript
var http := HTTPRequest.new()
add_child(http)
var headers := PackedStringArray(["Authorization: Bearer " + api_key, "Content-Type: application/json"])
var body := JSON.stringify({"nick_name": "Player1", "password": ""})
http.request("http://localhost:3000/rooms/%d/tickets" % room_id, headers, HTTPClient.METHOD_POST, body)
# Response JSON: var token: String = parsed["token"]
```

Next: [enet.md](enet.md) (primary) or [realtime.md](realtime.md#websocket-debug) (WS smoke test).
