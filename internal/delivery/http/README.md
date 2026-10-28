# delivery/http

REST control plane (Fiber v3), address — env `HTTP_ADDR` (default `:3000`).

Client spec (Godot): [docs/client-protocol/http.md](../../../docs/client-protocol/http.md).

## Authentication

All endpoints below are protected by an API key (`adapter/httpauth`) unless `HTTP_AUTH_DISABLED=true`.

Headers (one of):

- `Authorization: Bearer <HTTP_API_KEY>`
- `X-API-Key: <HTTP_API_KEY>`

Without a key or with an invalid key — `401` / `{"code":"unauthorized",…}`.

`/ws` and the data plane do not use this key (join — ticket).

## Limits

| Mechanism | Where | Response |
|-----------|-------|----------|
| Rate limit create/issue/delete/list | middleware per IP | `429` / `rate_limited` |
| Max rooms per instance | `Create` use case | `503` / `rooms_limit_reached` |

Env — [adapter/httplimits/README.md](../../adapter/httplimits/README.md).

## Endpoints

| Method | Path | Use case | Success | Request body | Response body |
|--------|------|----------|---------|--------------|---------------|
| `POST` | `/rooms` | Create | `201` | `capacity`, `nick_name`, `password?`, `attributes?` | `token`, `reconnect_token`, `enet_port?`, `room` (as in list) |
| `GET` | `/rooms` | GetList | `200` | — | `rooms[]` |
| `POST` | `/rooms/:id/leave` | Self-leave via `reconnect_token` (P32, P31) — `204`, `401 invalid_credentials` |
| `DELETE` | `/rooms/:id` | Delete | `204` | `password?` | — |
| `POST` | `/rooms/:id/tickets` | IssueTicket | `201` | `nick_name`, `password` | `token`, `reconnect_token`, `enet_port?`, `room` (as in create/list) |

`nick_name` is required on create and issue (`domain.ValidateNickName`). Error: `400` / `invalid_nick_name`.

`enet_port` (uint16, `omitempty`) - the assigned ENet host port for this ticket (P18 sharding; present only when the ENet listener is up and a host was chosen - otherwise the field is omitted). `reconnect_token` (string) - reconnect token for resuming under the same peerID (P20).

Room password: if set on create — required for IssueTicket and Delete.

## JSON errors

`{"code":"…","message":"…"}` — mapping in `errors.go` (`room_not_found`, `ticket_slot_held`, `invalid_nick_name`, `invalid_capacity`, …).

`POST /rooms`: `capacity` ≤ 0 or > `domain.MaxRoomCapacity` (127) → `400` / `invalid_capacity`. Master-migration mode is not a create parameter but a global deployment env `STATE_POLICY_ALLOW_MASTER_MIGRATION` (P33): `false` — host leaving = room death (`OpRoomClosed` `0x82`).

## Files

`rooms.go` — handlers; `auth_middleware.go` — API key; `rate_limit_middleware.go` — per-IP limits; `metrics_middleware.go` — `NewMetricsMiddleware`: the `HTTPRequestObserved` signal per route pattern (P19b; `/ws` upgrade requests are skipped); `dto.go` — JSON types; `mapper.go` — summaries; `parse.go`, `errors.go`.
