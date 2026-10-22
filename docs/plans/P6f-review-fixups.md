# P6f — Фиксы по итогам двухосевого ревью P6

## Контекст

Code-review волны P6 (Standards + Spec оси) нашёл два реальных дефекта и набор мелочей. Этот план — зафиксированный скоуп правок красной зоны (поручение пользователя: «запусти на всё отдельный сабагент»).

## Скоуп правок (override красной зоны)

- `cmd/rooms/main.go` — только блок listenErr/select (строки ~127-183)
- `internal/usecase/room/leave_room.go` — ветка ошибки `room.Leave`
- `internal/delivery/http/errors.go` — маппинг `ErrInvalidRoomCapacity`
- `internal/adapter/realtime/policy/state/server_state_room_policy.go` — `refreshPeerPings`
- `internal/adapter/reservation/reservation.go` — хелпер `checkLive` вместо ×6 `unregistered`
- `internal/usecase/room/sweep*.go` / `evict_idle_peers.go` — общий `startSweep`-хелпер (если не ломает публичный API `SweepHandle`)
- `internal/delivery/http/rate_limit_middleware.go` — общий конструктор `newRateLimitMiddleware(func(string) bool)` (если не ломает обработчики)
- `internal/adapter/realtime/codec/` — перенос `checkNoTrailingBytes` в `codec_util.go` (жёлтая, в рамках подтверждённого ранее codec-скоупа)
- `internal/domain/` — `domain.RoomID(0)` → `domain.RoomIDInvalid` где уместно
- `internal/adapter/reservation/reservation.go` — переменная `peer`→`slot` в Reserve-цикле
- `tests/**`, package README — под новое поведение

## Не трогать

- `internal/delivery/enet/**` кроме уже сделанного — P7
- `reservation` password hashing — голый sha256 принят сознательно (in-memory only, хэш не сериализуется и не утекает); формулировку в P6b пометить «bare sha256 — достаточно, соль бесполезна без внешней персистентности»
- `createMu` границы (пересмотр ширины — отдельно, если захочется)

## Дефекты (обязательно)

1. `main.go`: `listenResult` заполняется только на ошибочной ветке; если `Listen` выходит с `nil`/`ErrServiceUnavailable`, `<-listenErr` на ~178 виснет. Фикс: читать результат однократно и аккуратно (например `listenDone` + один консьюмер, или сохранять результат всегда).
2. `leave_room.go`: после D4 `room.Leave` удаляет peer из мапы даже при ошибке → `!HasPeer` → `peer.Stop()` пропускается и ошибка проглатывается. Фикс: при ошибке Leave всё равно `peer.Stop()` + `Revoke`, вернуть `errors.Join`.
3. `delivery/http/errors.go`: `ErrInvalidRoomCapacity` → `400 invalid_capacity`.
4. `refreshPeerPings`: вычищать stale-запись только на `errors.Is(err, domain.ErrPeerNotFound)`; при ошибке мастер-выборов не делать early return, а продолжить вычистку остальных (иначе stale остаются до следующего тика и master висит на удалённом peer).

## Гигиена (по желанию агента, если без ломки API)

- `checkNoTrailingBytes` → `internal/adapter/realtime/codec/codec_util.go`.
- `reservation.go`: `room.checkLive(roomID)`-хелпер; `peer`→`slot`.
- Общий `startSweep(ctx, interval, logger, run)` для двух sweep'ов; общий `newRateLimitMiddleware`.
- `p.master = oldMaster` ×4 → `defer` + committed-флаг.
- `sendFullStateToPeer`/`deliverFullStateToPeer` дубль — оставить, если ветвление заметно разное.
- `state_room_policy.go:50` `domain.RoomID(0)` → `domain.RoomIDInvalid`.

## Тесты

- `leave_room`: ошибка `Leave` → `Stop` вызван, Revoke вызван, ошибка возвращена (regression).
- HTTP create с capacity вне [1,127] на уровне use case → `invalid_capacity` маппинг (delivery-таблица).
- `refreshPeerPings` с двумя stale peers: оба вычищены, master не висит на удалённом.
- main.go listenErr-ветка — тестом не покрывается дёшево (composition root); достаточно ревью.

## Проверки

```bash
gofmt -w . && go build ./... && go vet ./...
~/go/bin/golangci-lint run ./...
go test -race -shuffle=on -count=1 ./tests/...
```
