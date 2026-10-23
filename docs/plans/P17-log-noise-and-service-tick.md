# P17: мелкие правки — send-error noise + ENet service tick

## 17.1 `error sending frame, closing connection` — понизить ожидаемую ошибку

`internal/adapter/realtime/peer.go:119` — `processOutgoingEvents` логирует
`Error` при любом фейле `Send`. При teardown/evict соединение закрывается
внешне (ctx cancel → conn.Close), а исходящая горутина успевает выбрать кадр
из очереди и получить `use of closed network connection` — это **ожидаемое**
завершение, не сбой. Шумит в e2e-логах и будет шуметь в проде.

Фикс: `errors.Is(err, net.ErrClosed)` (или обёртка) → `Debug`, прочие ошибки
записи → `Error` как сейчас. Проверить, что оба транспорта пробрасывают
`net.ErrClosed` наверх: WS (`*websocket.Conn.WriteMessage` → net.ErrClosed
проброшен?), ENet (Send через enqueue — вернёт `ErrHostLoopStopped` после
стопа, это тоже ожидаемый путь → Debug). Решить по факту, какие sentinel
ошибки считать «ожидаемым закрытием»: `net.ErrClosed`, `ErrHostLoopStopped`.

## 17.2 `ServiceTimeoutMs` дефолт 10ms → 1ms

`adapter/transport/enet.DefaultEnetConfig().ServiceTimeoutMs = 10`. После
P15 очередь команд опустошается только между Service-итерациями → send-path
латентность ≈ service timeout worst-case. 1ms: ~10x ниже latency команд,
цена ~1000 итераций/сек (тривиально для одного host'а; документировать для
многих host'ов в P18).

Проверить: env-переменная `ENET_SERVICE_TIMEOUT_MS` уже парсится?
(`adapter/config/load_enet.go`) — если нет, добавить. Дефолт 1, env может
поднять обратно.

## Скоуп

- `internal/adapter/realtime/peer.go` (log level)
- `internal/adapter/transport/enet/enet_config.go` (default)
- `internal/adapter/config/` (env var, если отсутствует)
- README пакетов по факту
- Этот план

## Верификация

- build/vet/lint, `-race -shuffle -short` сьют
- `./scripts/enet-verify.sh test` — tagged прогон (ServiceTimeoutMs=1 в env
  уже стоит, поведение не меняется)
- e2e-лог: `error sending frame` не должно быть на штатных закрытиях
