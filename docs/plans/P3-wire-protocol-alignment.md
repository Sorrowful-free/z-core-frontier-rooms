# P3 — Wire protocol alignment

> **Статус:** done (verified: build/vet/lint clean, go test -race -shuffle=on green).

## Контекст

Три расхождения кода с документированным wire-контрактом: `ErrRoomFull` уходит клиенту как `OpInternal`, flagless ENet-пакеты клиента мапятся не в `DeliveryUnreliable`, `dispatchLifecycle` возвращает «room stopped» без `ctx.Err()`.

## Находки

1. **`ErrRoomFull` → default → `OpInternal 0x50`** (delivery/errors/join_reject.go:33): `Room.Join` возвращает `ErrRoomFull` (room.go:127), обёрнутый в `ErrJoinDenied` (room.go:141). Специального опкода «комната полна» для room-слоя нет.
2. **ENet flagless ≠ `DeliveryUnreliable`**: `frameFromPacket` маппит delivery только при `GetFlags() != 0` (delivery/enet/frame_cgo.go:16-18) → flagless-пакет оставляет `frame.Delivery` нулём, а enet.md:130 требует: клиент→сервер без флага = `DeliveryUnreliable`. Сама `deliveryFromPacketFlags` (flags_cgo.go) уже возвращает `DeliveryUnreliable` при отсутствии `PacketFlagReliable` — баг именно в guard.
3. **`dispatchLifecycle` без `ctx.Err()`** (room_lifecycle.go:38,45): возвращает голый `fmt.Errorf("room stopped: %d")`, а realtime/README.md:36 требует «room stopped через `ctx.Err()`» (как `tryDeliver`, room.go).

## Решения (зафиксировано)

- `ErrRoomFull` → `OpReservationFull (0x71)` — клиент уже трактует `0x71` как «комната полна», новый опкод не вводим. В `JoinRejectOpCode` case `errors.Is(err, domain.ErrRoomFull)` ставить **выше** `ErrJoinDenied` — `ErrRoomFull` приходит обёрнутым в `ErrJoinDenied` и матчит оба.
- `frameFromPacket`: вызывать `deliveryFromPacketFlags` безусловно (убрать `GetFlags() != 0` guard) → flagless → `DeliveryUnreliable`.
- `dispatchLifecycle`: `fmt.Errorf("room stopped: %d: %w", r.id, r.ctx.Err())` в обеих точках.
- Документацию правим под факт кода где код правильный; код — под docs где docs правильные (случай 2: docs правильны, чиним код).

## Скоуп правок (override красной зоны — только эти файлы)

- `internal/delivery/errors/join_reject.go`
- `internal/delivery/enet/frame_cgo.go`
- `internal/adapter/realtime/room_lifecycle.go`
- `docs/client-protocol/errors.md`, `docs/client-protocol/enet.md`
- `.cursor/rules/join-error-opcodes.mdc`
- `internal/delivery/errors/README.md`, `internal/delivery/enet/README.md`
- `tests/delivery/errors/*_test.go`, `tests/delivery/enet/*_test.go`, `tests/adapter/realtime/*_test.go` (только под этот план)

## Не трогать (чужие планы)

- `internal/adapter/realtime/{peer,room,peer_factory}.go` — P1/P4
- `internal/adapter/realtime/codec/**`, `policy/state/**` — P2/P4
- `internal/adapter/transport/**` — P1
- `internal/domain/**` — sentinel и опкод уже есть; если вдруг нужен новый — стоп и отчёт
- `internal/adapter/realtime/README.md` — P4

## Шаги

1. `join_reject.go`: добавить `case stderrors.Is(err, domain.ErrRoomFull): return domain.OpReservationFull` **перед** `ErrJoinDenied`.
2. `frame_cgo.go`: убрать guard `GetFlags() != 0`.
3. `room_lifecycle.go`: `%w`-обёртка `ctx.Err()` в обеих ветках «room stopped».
4. Docs-синк: errors.md — пометить `0x71` = `ErrReservationFull` **и** `ErrRoomFull`; правило join-error-opcodes.mdc — строка про room-full; README delivery/errors; enet.md — проверить, что строка про flagless=unreliable соответствует коду после фикса (править только при расхождении).

## Тесты

- `tests/delivery/errors/join_reject_test.go`: `ErrRoomFull` (в т.ч. обёрнутый в `ErrJoinDenied`) → `OpReservationFull`, не `OpInternal` и не `OpJoinDenied` — regression-тест.
- `tests/delivery/enet/`: flag-mapping — если тестируемо без cgo/host, добавить; иначе пометить покрытием ENet smoke-джобой.
- `dispatchLifecycle`: существующие room_stop-тесты должны проходить; при возможности — assert `errors.Is(err, context.Canceled)` на «room stopped».

## Docs

- `docs/client-protocol/errors.md` — таблица `0x71`: добавить `ErrRoomFull` в колонку sentinel (строка «комната полна» уже есть).
- `.cursor/rules/join-error-opcodes.mdc` — синк таблицы/правила.
- `internal/delivery/errors/README.md` — маппинг-таблица.
- `docs/client-protocol/enet.md` — только сверка, факт уже документирован.

## Проверки

```bash
go build ./... && go vet ./... && gofmt -l internal/delivery/ internal/adapter/realtime/
go test -shuffle=on -count=1 ./tests/delivery/... ./tests/adapter/realtime/...
go test -race -count=3 ./tests/delivery/errors/ ./tests/integration/
```
