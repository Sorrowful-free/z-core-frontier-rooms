# P16: гигиена тестового кода и мелкие хвосты

## Состав (leftovers аудита)

### 16.1 Консолидация рукописных peer-стабов

~15 hand-written стабов одного интерфейса `realtime.Peer` размножились по
тестам: `stubPeer` (room_join_test), `capturePeer`, `flakyCapturePeer`,
`mortalPeer`, shadowPeer и др. в policy/state, плюс стабы в других пакетах.
P13 уже начал схождение (capturePeer получил setErr). Варианты:

Решение (grill Q3, вариант a): один параметризуемый test-peer в
`tests/testutil/` (id/nick/ping/alive/deliverErr/capture-собранные кадры) —
стабы сводятся к конфигурации. gomock не используем: capture-стиль не
ложится на EXPECT.

### 16.2 Vendor-тип `*websocket.Conn` в WS-порту

`port/transport/ws.CreateConnection(conn *websocket.Conn)` тащит
contrib-тип в port-контракт. Решение (grill Q4, вариант a): **оставить** —
тип принадлежит транспорту, port-контракт честно отражает зависимость;
интерфейс дал бы ложную абстракцию с единственной реализацией.

### 16.3 Мелочи

- `WithRoomPatchInterval` — не используется после P13: удалить или оставить
- `leave room: peer already left` — гонка evict/deferred-leave, безвредный
  шум: понизить уровень лога
- P12-файл `match_e2e_enet_test.go`: skip-комментарий «~90-100s» vs факт ~115s

## Границы

- Зелёная зона (`tests/**`, docs) — свободно
- Прод-код только если выбран вариант с `port/transport/ws` — тогда scope
  ограничен сигнатурой factory

## Верификация

- `go test -race -shuffle=on -count=1 ./tests/...` + tagged Docker прогон
