# adapter/httpauth

HTTP control plane authentication configuration.

## HTTPAuthConfig

| Field | Description |
|-------|-------------|
| `APIKey` | REST secret; `Authorization: Bearer …` or `X-API-Key` header |
| `Disabled` | `true` — middleware lets all requests through (dev only) |

`Validate()` — the key is required and ≥ 16 bytes if auth is not disabled.

Env and loading — [adapter/config/README.md](../config/README.md).

Middleware — [delivery/http/README.md](../../delivery/http/README.md).
