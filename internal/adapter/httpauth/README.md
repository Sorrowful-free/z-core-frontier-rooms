# adapter/httpauth

Конфигурация аутентификации HTTP control plane.

## HTTPAuthConfig

| Поле | Описание |
|------|----------|
| `APIKey` | Секрет для REST; заголовок `Authorization: Bearer …` или `X-API-Key` |
| `Disabled` | `true` — middleware пропускает все запросы (только dev) |

`Validate()` — ключ обязателен и ≥ 16 байт, если auth не отключён.

Env и загрузка — [adapter/config/README.md](../config/README.md).

Middleware — [delivery/http/README.md](../../delivery/http/README.md).
