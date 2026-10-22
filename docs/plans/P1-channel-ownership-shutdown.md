# P1 — Channel ownership на shutdown

> **Статус:** done (verified: build/vet/lint clean, go test -race -shuffle=on green).

## Контекст

Ревью realtime-адаптеров нашло два потенциальных `send on closed channel` panic и один незавершённый lifecycle в ping-loop. Контракт из `internal/adapter/realtime/README.md` уже запрещает закрытие room-каналов по этой причине — peer/enet ему противоречат.

## Находки

1. `Peer.Stop` → `close(p.outbound)` (peer.go:76): продюсеры `tryDeliver` (peer.go:123-135) **не входят** в `p.wg`. TOCTOU: `tryDeliver` прошёл `ctx.Err()` → `Stop` отменил ctx и закрыл канал → select может выбрать send → panic.
2. `EnetConnection.Close` → `close(c.incoming)` (connection_cgo.go:54): `peerSession.deliver` (delivery/enet/session_cgo.go:36-43) шлёт в тот же канал из host-цикла → та же panic-пара.
3. `WsConnection.runPingLoop` выходит по ошибке `WriteControl` (connection.go:124), не помечая conn закрытым → `IsAlive()` остаётся `true`, zombie.
4. Косметика: `PeerFactory` держит `logger` полем + per-call параметр с nil-fallback (peer_factory.go:29-31) — два источника logger.

## Решения (зафиксировано)

- **Не закрывать** `p.outbound` и `c.incoming` вообще. Consumers выходят по done-сигналу, а не по закрытию канала.
- `processOutgoingEvents` уже выходит по `peer.ctx.Done()` — удаление `close(p.outbound)` безопасно; `tryDeliver` после stop отдаёт `ErrQueueFull`/«peer stopped» — без panic.
- В `EnetConnection` добавить `closed chan struct{}` (зеркаля паттерн `WsConnection.closed`): `Receive` → `select { case <-c.closed: return EOF; case frame := <-c.incoming: return frame }`. `Close` закрывает `closed`, `incoming` не трогает; `peerSession.deliver` продолжает non-blocking send — буфер заполнится и дропнется, канал соберётся GC вместе с session. `c.alive` оставить как есть.
- `runPingLoop`: при ошибке `WriteControl` → `c.Close()` перед return (closeOnce делает идемпотентно) — `closed` закроется, `IsAlive` корректно уйдёт в false.
- `PeerFactory`: оставить как есть ИЛИ убрать nil-fallback (требовать non-nil logger). **Не менять сигнатуру порта** `internal/port/realtime` — если правка требует смены сигнатуры, пропустить пункт и отметить в отчёте.
- Решение «не закрывать каналы» фиксируется ADR-0003.

## Скоуп правок (override красной зоны — только эти файлы)

- `internal/adapter/realtime/peer.go`
- `internal/adapter/realtime/peer_factory.go`
- `internal/adapter/transport/enet/connection_cgo.go`
- `internal/adapter/transport/ws/connection.go`
- `docs/adr/0003-channel-ownership.md` (новый файл)
- `tests/adapter/realtime/peer_*.go`, `tests/adapter/realtime/*_test.go` (только новые/существующие тесты под этот фикс)
- `internal/adapter/transport/README.md`, `internal/delivery/enet/README.md` — только если там описано закрытие `incoming` (проверить, при необходимости строка про done-канал)

## Не трогать (чужие планы)

- `internal/adapter/realtime/room.go`, `room_lifecycle.go` — P4/P3
- `internal/adapter/realtime/codec/**`, `policy/state/**` — P2/P4
- `internal/adapter/realtime/README.md` — P4
- `internal/delivery/enet/frame_cgo.go`, `internal/delivery/errors/**` — P3
- `internal/port/**` — без явного добро

## Шаги

1. `peer.go`: убрать `close(p.outbound)` из `Stop` (оставить `cancel` → `connection.Close()` → `wg.Wait()`).
2. `connection_cgo.go`: добавить `closed chan struct{}` в struct и конструктор; `Receive` — select на `closed`/`incoming`; `Close` — `close(c.closed)` вместо `close(c.incoming)`.
3. `connection.go` (ws): в `runPingLoop` при ошибке `WriteControl` вызвать `c.Close()` до return.
4. `peer_factory.go`: nil-fallback logger — убрать или оставить (см. решения), отчитаться что выбрано.
5. Написать `docs/adr/0003-channel-ownership.md` (формат как у 0001/0002): контекст — send-on-closed panics; решение — каналы outbound/incoming никогда не закрываются, consumers завершаются по ctx/done-каналу; последствия — буфер мёртвого пира дренится GC, senders получают `ErrQueueFull`.

## Тесты

- `tests/adapter/realtime/`: тест «`Stop` конкурентно с `Deliver` не паникует» — N горутин `Deliver` в цикле + `Stop`; прогон под `-race -count=5`. До фикса — потенциальный panic, после — ошибка/ok.
- Проверить существующие `peer_stop_test.go`, `room_stop_test.go`, `queue_drop_test.go` — семантика не должна сломаться.
- ENet-ветка (cgo) покрывается smoke-джобой CI; локально без libenet — только `go vet`/`build` с тегом невозможен, отметить в отчёте.

## Docs

- Новый `docs/adr/0003-channel-ownership.md` (шаг 5).
- Проверить `internal/adapter/transport/README.md` / `internal/delivery/enet/README.md` на упоминание закрытия `incoming` — синкнуть при наличии.

## Проверки

```bash
go build ./... && go vet ./... && gofmt -l internal/adapter/...
go test -race -shuffle=on -count=1 ./tests/...
go test -race -count=5 ./tests/adapter/realtime/
```
