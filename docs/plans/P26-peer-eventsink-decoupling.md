# P26 — decouple Peer → Room до EventSink (Start-инъекция)

## Контекст

`Peer` (adapter/realtime/peer.go) хранит `room realtime.Room` — полный
порт комнаты (18 методов) — но использует ровно `Deliver(events.RoomEvent)`
в read-loop (peer.go:97). Топология `peer read-loop → room.incoming`
mailbox НЕ меняется; меняется форма зависимости.

Загриллено (модель A+): peer-структура не хранит НИЧЕГО комнатного —
sink передаётся в `Start` и живёт только в замыкании read-горутины.
Отвергнуты: поле EventSink через конструктор (ссылка всё равно на
struct'е); сырой `chan<-` (теряет drop-политику/метрики/ctx-check
`tryDeliver`); room-owned ingress pump (C — перенос ownership read-
цикла к комнате: протечка conn наружу или RunInbound = тот же Start,
плюс бухгалтерия пампов и двойной wg); mailbox pull (room не может
select по динамическому множеству каналов → вырождается в push-
форвардер = текущий read-loop).

## Решения

### Порт

- `internal/port/realtime/`: новый интерфейс

```go
// EventSink — узкая точка доставки входящих кадров в комнату
// (mailbox room.incoming); реализует Room. Peer получает sink
// в Start и не хранит ссылки на комнату (P26).
type EventSink interface {
    Deliver(events.RoomEvent) error
}
```

- `Peer` интерфейс: `Start(sink EventSink) error` (было `Start() error`).
- **Контракт порядка (Start-after-join)**: `Start` вызывается только
  после успешного `Join`/`Replace` — sink это не «какая-то комната»,
  а mailbox той комнаты, которая peer'а уже приняла. Peer сам
  проверить членство не может (о комнатах он больше не знает — и это
  намеренно): инвариант caller-side, документируется в порту;
  placePeer уже соблюдает порядок (Start — последний шаг, фейл →
  rollback Leave+Stop).
- **Контракт Stop**: завершение обеих горутин реальное, не
  fire-and-forget — `cancel()` → `conn.Close` → read-loop выходит по
  ошибке `Receive`, write-loop по `ctx.Done`, `wg.Wait()` их ждёт
  (уже реализовано — фиксируется как контракт порта). Порядок: Stop
  после изъятия из комнаты (Leave/Replace/rollback) — остановленный
  peer в `r.peers` = zombie до idle-sweep. Sink умирает с read-
  горутиной — утечки ссылки на комнату нет. Fallback: conn умер сам
  (сеть) → read-loop выходит по ошибке `Receive` без Stop; write-loop
  висит на ctx.Done до Stop от evict-пути — поведение без изменений.
- `PeerFactory.CreatePeer`: параметр `room Room` **удаляется** —
  конструкции peer'а комната не нужна (сигнатура:
  `CreatePeer(ctx, id, nickName, connection, logger)`).

### Адаптер

- `peer.go`: поле `room` удалено; `NewPeer` без `room`;
  `processIncomingEvents(p, sink)` — sink параметром goroutine;
  `p.Start(sink EventSink)` спавнит read-loop с sink.
- `room.go`: `var _ EventSink = (*Room)(nil)` (Deliver уже есть).
- `peer_factory.go`: `CreatePeer` без `room` (ctx по-прежнему —
  `room.Context()` от caller'а, отдельный аргумент).

### Usecase

- `place_peer.go`: `CreatePeer(room.Context(), peerID, nick, conn, logger)`;
  `peer.Start(room)` — room передаётся как EventSink в точке спауна
  (порядок Join/Replace → Start не меняется).

### Тесты

- `tests/mocks/` — regen (`go generate ./tests/mocks/...`);
  `peer.EXPECT().Start()` → `Start(gomock.Any())` / конкретный sink;
  `CreatePeer` EXPECT'ы — без room-аргумента.
- `NewPeer(...)` прямые конструкции в `tests/adapter/realtime/` —
  без room-аргумента (nil-room кейсы просто теряют параметр).
- Новый тест по желанию: peer read-loop шлёт кадры в переданный sink
  (fake sink-стаб), не в Room — фиксирует контракт.

### Docs

- `internal/adapter/realtime/README.md`, `internal/port/realtime`
  (doc-комментарии): peer не хранит room; EventSink — входная точка
  mailbox'а; ctx жизни peer'а — отдельным аргументом конструктора.

## Скоуп правок (фиксированный)

- `internal/port/realtime/` (`peer.go`, `peer_factory.go`, + файл
  или секция с `EventSink` — напр. `eventsink.go`)
- `internal/adapter/realtime/` (`peer.go`, `peer_factory.go`,
  `room.go` — только var-ассёрт)
- `internal/usecase/room/place_peer.go`
- `tests/mocks/` (regen), `tests/adapter/realtime/` (NewPeer callsites,
  Start EXPECT'ы), `tests/usecase/room/` (CreatePeer EXPECT'ы)
- `internal/adapter/realtime/README.md`

## Верификация (для сабагента)

- `go build ./...` — 0 ошибок.
- `go vet ./...` — 0.
- `~/go/bin/golangci-lint run ./...` — 0 issues.
- `go generate ./tests/mocks/...` — моки перегенерированы.
- `go test -race -shuffle=on -short -count=1 ./tests/...` — зелёные.
- `grep -n "room" internal/adapter/realtime/peer.go` — ссылок на
  realtime.Room в peer.go не осталось.
