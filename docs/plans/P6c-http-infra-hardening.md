# P6c — HTTP/infra hardening

> **Статус:** done (verified: build/vet/lint clean, go test -race -shuffle=on green).

## Контекст

DoS-дыры и infra-гонки аудита: `DELETE /rooms/:id` без rate limit при проверке пароля (брутфорс-оракул), неограниченный `windows` в rate limiter (IP-ротация → утечка памяти), нет Recover middleware (паника в handler роняет процесс), check-then-act в `CreateRoom`, `DeleteRoom` держит write-лок через `room.Stop()`, sweep-горутины fire-and-forget, stdlib-логгер ломает kv-семантику.

## Зависит от

— (независим; файлы не пересекаются с P6a/P6b/P6d)

## Скоуп правок (override красной зоны)

- `internal/delivery/http/rooms.go`, `internal/delivery/http/rate_limit_middleware.go`
- `internal/adapter/httplimits/*.go` (+ `port/httplimits` если нужен метод лимита для delete/list)
- `internal/adapter/registry/room_registry.go`
- `internal/usecase/room/{evict_idle_peers,sweep_orphan_admitted}.go`
- `internal/adapter/logging/stdlib/logger.go`
- `cmd/rooms/main.go` — **только** recover middleware, sweep-wait, listen-error path
- README затронутых пакетов; `tests/**` соответствующих пакетов

## Не трогать

- `reservation`, `admission`, `issue_ticket`, `join_room` — P6b. `realtime` — P6a. `codec` — P6d.

## Шаги

1. `rooms.go:120-121`: rate limit middleware на `DELETE /rooms/:id` и `GET /rooms` — новый лимит в `HTTPLimitsConfig` (напр. `HTTP_DELETE_RATE_PER_MIN`, `HTTP_LIST_RATE_PER_MIN`) по config-паттерну (тип в adapter + `Validate()` + `load.go` + `.env.example` + README).
2. `rate_limiter.go`: eviction — lazy sweep просроченных `windows` внутри `allow` (напр. каждые N вызовов или по `lastSweep` таймстампу); **без** новой горутины. Комментарий про fixed-window 2×-burst; поведение `c.IP()`/proxy — задокументировать в `httplimits/README.md`.
3. `main.go`: `recover.New()` middleware до роутов (fiber middleware); при ошибке `fiberApp.Listen` non-`ErrServiceUnavailable` — выполнить cleanup (registry shutdown), не просто `return err`; `signal.Stop(sigCh)` после выхода из select.
4. `evict_idle_peers.go`, `sweep_orphan_admitted.go`: `Start*` возвращает handle (`Stop()` + внутренний `WaitGroup`) — стандарт «Stop дожидается реального завершения»; `main.go` держит handles и ждёт после `appCancel()`. Заодно в `sweep_orphan_admitted.go:59-65`: `continue` на битой комнате вместо abort всего sweep.
5. `room_registry.go` `CreateRoom`: re-check `r.rooms[id]` под write-локом; проигравшая комната → `Stop()` вне лока. `DeleteRoom`: `delete(r.rooms, id)` под локом, `room.Stop()` вне лока (ошибка stop — лог, комната всё равно снята с реестра).
6. `stdlib/logger.go`: kv-семантика как zap-адаптер — `msg` + `key=value` пары в суффиксе (не `Sprintf(msg, args...)`); `Fatal` добавить `l.tag`.

## Тесты

- Rate limit на DELETE/GET: N+1 запрос → `429`; eviction: после window-экспайра старые ключи удаляются из `windows` (инспектировать через поведение — рост мапы ограничен).
- `CreateRoom` ×2 конкурентно с одним id → `ErrRoomAlreadyExists` одному, нет утечки (комната одна в `GetList`, горутин двух нет — по `goleak`-подобному счёту или Stop-ветке).
- `DeleteRoom` во время медленного `Stop` — `GetList` не блокируется (тест с политикой-затором).
- Sweep-handle: `Stop()` → `RunOnce` in-flight завершился до возврата.
- Logger conformance: одинаковый вызов на stdlib/zap даёт сопоставимый вывод (`"k", v` не ломает формат).
- Существующие `tests/delivery/http`, `tests/adapter/registry`, `tests/adapter/httplimits` — зелёные.

## Проверки

```bash
go build ./... && go test -race -count=3 ./tests/delivery/http/... ./tests/adapter/registry/... ./tests/adapter/httplimits/... ./tests/usecase/room/...
```
