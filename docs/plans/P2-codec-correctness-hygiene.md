# P2 — Codec correctness & hygiene

## Контекст

Ревью codec-пакета (`internal/adapter/realtime/codec`) нашло баг silent truncation на decode, кириллицу в идентификаторе, дубли и мёртвый код. Зона codec — жёлтая: план исполняется после отдельного «да», блоками с логом в чате.

## Находки

1. **Silent truncation**: `buf.Read(data)` может вернуть меньше `len(data)` без ошибки (`bytes.Buffer` семантика) → `readString` (string_codec.go:23-26) и `readValueState` (value_state_codec.go:46-49) декодируют укороченные данные как валидные. Нужен `io.ReadFull`.
2. **Мёртвая проверка**: `valueLen > math.MaxUint8` на `byte` (value_state_codec.go:43) — всегда false.
3. **Кириллица в идентификаторе**: `writeMapPatсhState` — `с` кириллическая (map_state_codec.go:58); call-site'ы: component_codec.go:55, entity_codec.go:51,147, input_state_codec.go:66, room_state_codec.go:155,166,176.
4. **Pointer to interface**: `RoomStateCodec.logger *logging.Logger` (room_state_codec.go:27) — поле не используется; конструктор `codec.NewRoomStateCodec(&f.logger)` (state_room_policy_factory.go:22).
5. **Дубли**: inline-замыкания чтения `ValueId`/`ValueState` дублируют `readValueID`/`readValueState` в component_codec.go:35-47, input_state_codec.go:46-58; `(*V, error)`-обёртка повторяется ~6 раз; `EntitiesStateMaxEntities` == `RoomStateMaxEntities` == 65535 (entity_codec.go:13, room_state_codec.go:15) — унифицировать константу или задокументировать совпадение.
6. **Dead code**: `writeInputID`/`readInputID` (input_state_codec.go:22-32).
7. **Несогласованность**: `&codec.RpcStateCodec{}` литерал рядом с конструкторами (state_room_policy_factory.go:25) — добавить `NewRpcStateCodec()` для единообразия.

## Решения (зафиксировано)

- `io.ReadFull` в обоих местах чтения по длине; проверку из п.2 удалить (лимит уже покрыт типом byte / валидацией выше).
- Переименование `writeMapPatсhState` → `writeMapPatchState` во всех call-site'ах одной правкой.
- `RoomStateCodec.logger` удалить целиком: поле, параметр конструктора, `&f.logger` в фабрике. Сигнатура `NewRoomStateCodec()` без аргументов.
- Дедуп: вынести общие read-хелперы в `value_state_codec.go` (или где уже лежат `readValueID`/`readValueState`), замкнуть замыкания на них. `NewRpcStateCodec()` — добавить.
- Бинарный wire-формат **не меняется** — это refactor + исправление отказоустойчивости decode.

## Скоуп правок (жёлтая зона codec + соседние файлы по необходимости)

- `internal/adapter/realtime/codec/*.go` — все файлы пакета
- `internal/adapter/realtime/policy/state/state_room_policy_factory.go` (только строки вызова кодеков)
- `tests/adapter/realtime/codec/*_test.go` — новые/существующие тесты
- `internal/adapter/realtime/codec/README.md` — только если там описаны затронутые API (проверить)

## Не трогать (чужие планы)

- `internal/adapter/realtime/peer.go`, `peer_factory.go` — P1
- `internal/adapter/realtime/room.go`, `room_lifecycle.go` — P4/P3
- `internal/adapter/realtime/policy/state/{server_,client_,}state_room_policy.go`, `master_election.go` — P4 (кроме factory-файла, он в скоупе P2)
- `internal/adapter/transport/**`, `internal/delivery/**` — P1/P3

## Шаги

1. Rename `writeMapPatсhState` → `writeMapPatchState` (определение + все вызовы).
2. `string_codec.go`, `value_state_codec.go`: `io.ReadFull` + удаление мёртвой проверки.
3. Дедуп read-замыканий → общие хелперы; `EntitiesStateMaxEntities`/`RoomStateMaxEntities` — унифицировать или комментарий к совпадению (судить по месту, отчитаться).
4. `room_state_codec.go`: убрать `logger`; `input_state_codec.go`: убрать `writeInputID`/`readInputID`; `rpc_state_codec.go`: `NewRpcStateCodec()`; `state_room_policy_factory.go`: вызовы без `&f.logger`, через конструктор.
5. Прогнать существующие codec-тесты и бенчи (`*_bench_test.go`) — поведение encode не должно измениться.

## Тесты

- Новый кейс в `tests/adapter/realtime/codec/`: укороченный payload для string/value → `error`, а не тихий успех с короткими данными (regression на silent truncation).
- Существующие codec-тесты должны остаться зелёными без правок (wire-формат не менялся).

## Docs

- `internal/adapter/realtime/codec/README.md`: только если упоминаются изменённые типы/хелперы — синкнуть; wire-layout docs не трогаем (формат не изменился).

## Проверки

```bash
go build ./... && go vet ./... && gofmt -l internal/adapter/realtime/codec/
go test -shuffle=on -count=1 ./tests/adapter/realtime/codec/... ./tests/...
go test -race -count=3 ./tests/adapter/realtime/codec/
grep -rn 'Pat' internal/adapter/realtime/codec/ | grep -v 'Patch' # кириллицы не осталось
```
