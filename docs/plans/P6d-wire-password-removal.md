# P6d — Wire: удаление Password (D2) + codec hygiene

> **Статус:** done (verified: build/vet/lint clean, go test -race -shuffle=on green).

## Контекст

D2 зафиксировано: `Password` выкидываем из wire-формата `RoomState`/`RoomStatePatch`. Breaking change протокола — Godot-клиент обновляется синхронно (сторона пользователя). **Пароль комнаты остаётся** серверным секретом в `reservation` — на wire его быть не должно никогда. Заодно (раз уж в codec): декодеры отвергают trailing bytes — сейчас `payload+garbage` принимается молча.

## Зависит от

— (независим; жёлтая зона codec — скоуп ниже = «да» пользователя, идти логическими блоками с логом в чате)

## Скоуп правок

- `internal/domain/state/room_state.go` — поле `Password` из `RoomState`/`RoomStatePatch`, все точки касания (`NewRoomState`, `Clone`, `MakePatch`, `ApplyPatch`, `MakeRoomStatePatch`)
- `internal/adapter/realtime/codec/room_state_codec.go` — убрать `writeString(password)`/`readString` + флаг `RoomStateFlagsPassword`; **все** decode-функции пакета — reject при `buf.Len() > 0` после разбора
- `internal/adapter/realtime/policy/state/state_room_policy.go` — `syncRoomStateFromRoom` передаёт `""` в `NewRoomState` — сигнатура поменяется
- Wire-docs: `docs/wire-state-codec.md`, `internal/adapter/realtime/codec/README.md`, `docs/client-protocol/{enet,realtime,state-sync}.md` — удалить поле из layout, пометка «breaking»
- `tests/adapter/realtime/codec/**`, `tests/domain/state/**` — обновить под новый layout + regression «пароль не сериализуется»
- `internal/domain/state/README.md`, `internal/domain/README.md` — если Password описан

## Не трогать

- Остальной codec-смысл (флаги/порядок других полей) — только Password и trailing-bytes.
- `policy/state` кроме адаптации под новую сигнатуру `NewRoomState`.

## Шаги

1. `room_state.go`: убрать `Password string` из `RoomState`, `*string` из `RoomStatePatch`; вычистить `NewRoomState` (сигнатура без пароля — проверить все call-site: policy, тесты, `wire-state-codec.md`-примеры), `MakePatch`/`ApplyPatch`/`Clone`.
2. `room_state_codec.go`: удалить запись `Password` в `Encode` (после `capacity`), чтение в `Decode`, флаг `RoomStateFlagsPassword` + ветки в `EncodePatch`/`DecodePatch`. **Проверить сдвиг флагов**: `RoomStateFlagsPeers` и выше сдвигаются на бит — это и есть breaking change, зафиксировать в doc.
3. Все `Decode*` пакета: после разбора `if buf.Len() > 0 → error` (trailing bytes reject). Проверить, что bench-тесты не сломались.
4. `state_room_policy.go` `syncRoomStateFromRoom`: `NewRoomState(id, capacity)` без `""`.
5. Docs: `wire-state-codec.md` — новый layout (поле удалено, флаги сдвинуты); `client-protocol/*` — пометка breaking change; `codec/README.md`.

## Тесты

- Encode/Decode round-trip без поля; `Encode` комнаты не содержит пароль-байты (регрессионный тест «state никогда не несёт секрет»).
- `DecodePatch` со старым флагом `0x02` (бывший Password) → ошибка/корректный разбор нового layout — зафиксировать поведение.
- Trailing bytes: `append(valid, 0xFF)` → `Decode`/`DecodePatch` ошибка во всех codec-файлах.
- Существующие codec-тесты зелёные после адаптации fixture'ов.

## Проверки

```bash
go build ./... && go test -race -count=3 ./tests/adapter/realtime/codec/... ./tests/domain/state/... ./tests/adapter/realtime/policy/...
```
