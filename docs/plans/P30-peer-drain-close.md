# P30 — drain-aware peer close (Send до Close вместо Gosched best-effort)

## Контекст

ADR 0011 (Consequences) зафиксировал дыру: на WS `0x82 OpRoomClosed`
доставляется best-effort — `peer.Deliver` коммитит кадр в `outbound`,
а `peer.Stop` закрывает conn из другой горутины. `closeRoom` компенсирует
это `runtime.Gosched()` («уступаем квант write-loop'у») — приём, а не
гарантия. ADR прямо называет drain-aware close «отдельной работой».

Механика дыры (`internal/adapter/realtime/peer.go`):

- `Stop()` = `cancel()` → `conn.Close()` → `wg.Wait()`.
- Write-loop (`processOutgoingEvents`) выходит по `ctx.Done()` **даже
  с непустым `outbound`** — select-недетерминизм: кадр может быть
  отправлен, а может и нет.
- Дыра шире `0x82`: любой teardown (`evict`, `leave`, `kick`) может
  потерять последние кадры очереди.
- На ENet «жёсткий порядок» тоже частичный: `SendBytes` попадает в FIFO
  host loop'а до `DisconnectLater` только если write-loop успел dequeue
  кадра до `ctx.Done` — та же гонка на уровень выше.

Побочный эффект: `TestIntegration_AuthoritativeHost_RoomDiesWithHost`
опирается на `Gosched` — потенциальный флак под нагрузкой/CI.

Ещё хвост из ADR: `room.Leave` пишет `Error "policy on leave failed"`
при `ErrRoomShouldClose` — разовый шум на каждую смерть auth-комнаты
(sentinel — заказанный сигнал policy, не сбой leave; фильтр уже есть
для `ErrPeerNotFound`/`ErrPeerStale`).

## Загрилленные решения (уже согласованы)

1. **Drain внутри `Peer.Stop`** — универсально для всех teardown-путей
   (closeRoom, evict, leave, kick), не узкий `DeliverFinal`/ack-API.
2. **Bounded**: `TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT` в `TransportConfig`
   (`durationFromEnv`, как остальные transport-knobs), default `1s`;
   `0` — drain выключен (прежнее поведение, для тестов).
3. **Гарантия = «`Send` вызван до `Close`»**, не «клиент получил»: после
   нормального close() ядро досылает буфер; на ENet `SendBytes`
   гарантированно оказывается в FIFO до `DisconnectLater`.
   По дедлайну — force `conn.Close`, прерывающий даже заблокированный
   `Send` (WS write deadline 10s не должен растягивать Stop).
4. **Лог-фикс в том же плане**: `ErrRoomShouldClose` добавляется в
   sentinel-фильтр `room.Leave` — не Error.
5. Reason-payload у `0x82` — **не делаем** (ADR 0011 отложил, причина
   одна). Обработка `0x82` в Godot — вне репо.

## Зона / override

План с явным списком путей = зафиксированный скоуп override по
ai-companion (prod — красная зона). Список «файлы» ниже — единственное
разрешённое к правкам.

## Дизайн

### Peer: drain-фаза write-loop'а

- `Peer` получает поля `drainTimeout time.Duration` и
  `writeDone chan struct{}` (замыкается write-loop'ом при выходе).
- `NewPeer` — **сигнатура не меняется** (стабильна для white-box тестов,
  см. realtime/README): `drainTimeout` = пакетный дефолт
  `defaultPeerCloseDrainTimeout = 1 * time.Second`; продовое значение
  `PeerFactory` вливает полем — тот же паттерн, что `peer.metrics`.
- `outbound` **не закрывается** (ADR 0003: продюсеры вне `p.wg`).
- Write-loop: по `<-ctx.Done()` переходит в drain-фазу — сливает
  `outbound` до пустоты (`select`+`default`), затем возвращается.
  Ошибка `Send` в drain-фазе — conn мёртв, drain прекращается
  (тот же путь `connection.Close()` + выход).

### Peer.Stop: новый порядок

```go
p.stopOnce.Do(func() {
    p.cancel()                       // продюсеры Deliver теперь фейлятся
    if p.drainTimeout > 0 {          // ждём слив, bounded
        timer := time.NewTimer(p.drainTimeout)
        select {
        case <-p.writeDone:
        case <-timer.C:              // дедлайн — идём на force close
        }
        timer.Stop()
    }
    err = p.connection.Close()       // прерывает заблокированный Send
    p.wg.Wait()                      // read-loop выходит по Receive-ошибке
})
```

- Отдельный `writeDone` обязателен: ждать весь `wg` до `conn.Close`
  нельзя — read-loop висит в `Receive()` до close (deadlock).
- Дедлайн → `conn.Close` → `Send` возвращает ошибку → write-loop
  выходит → `wg.Wait` его подбирает. Hang исключён.
- ENet: `Send` — async enqueue в host loop (не блокирует); drain даёт
  строгий порядок `SendBytes` → `DisconnectLater` в одном FIFO —
  делает гарантию де-юре, а не «в практике».

### Контракт гонки (фиксируем в docs)

Кадры, чей `Deliver` закоммитил **до** начала `Stop`, получают попытку
`Send` в пределах `drainTimeout`. `Deliver`, гонящийся со `Stop`
(прошёл ctx-проверку, но enqueue после наблюдения пустой очереди
write-loop'ом) — **unspecified**, кадр может быть потерян. Это честная
граница: «последний кадр» семантически тот, что закоммичен до teardown.

### Конфиг

- `transport_config.go`: поле `PeerCloseDrainTimeout time.Duration`,
  `DefaultPeerCloseDrainTimeout = 1 * time.Second`, `Validate()`:
  `>= 0` (`0` — drain отключён, семантика «механизм выключен» как у
  `PeerIdleTimeout <= 0`).
- `load.go`: `TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT` через
  `durationFromEnv` (формат `1s`/`500ms`).

### PeerFactory

`NewPeerFactory(logger, outboundQueue, drainTimeout, m)` — сигнатура
растёт одним параметром; `CreatePeer` инжектит `peer.drainTimeout =
f.drainTimeout` (как `peer.metrics`). Точки wiring:
`cmd/rooms/main.go:97` (`cfg.Transport.PeerCloseDrainTimeout`),
`tests/testutil/env/env.go:185`.

### Лог-фильтр room.Leave

`room.go:181` — добавить `domain.ErrRoomShouldClose` в sentinel-фильтр
(рядом с `ErrPeerNotFound`/`ErrPeerStale`) + обновить комментарий:
sentinel — заказанный сигнал policy на teardown, обрабатывается
usecase'ом (closeRoom пишет свой `Info "room closed by policy"`),
Error тут ложный.

### closeRoom: убрать Gosched

`close_room.go`: удалить `runtime.Gosched()` и импорт `runtime`;
комментарий переписать — порядок «0x82 до close» теперь гарантирует
drain-aware `peer.Stop`. Порядок шагов (Deliver → DeleteRoom →
UnregisterRoom → Stop) **сохраняется**: он нужен для других
инвариантов (peer ctx от `room.Context()`; transport-колбэк должен
упереться в `ErrRoomNotFound`).

## Файлы (скоуп)

### Prod (красная, override этим планом)

- `internal/adapter/realtime/peer.go` — `drainTimeout`, `writeDone`,
  drain-фаза write-loop'а, новый `Stop`, пакетный дефолт.
- `internal/adapter/realtime/peer_factory.go` — параметр drainTimeout +
  injection полем.
- `internal/adapter/realtime/room.go` — `ErrRoomShouldClose` в фильтр
  `Leave` + комментарий.
- `internal/usecase/room/close_room.go` — убрать `Gosched`, комментарий
  на drain-гарантию.
- `internal/port/realtime/peer.go` — док-комментарий `Stop`: контракт
  «cancel → drain (bounded) → conn.Close → wg.Wait» + гонка Deliver.
- `internal/adapter/transport/transport_config.go` — поле, default,
  `Validate()`.
- `internal/adapter/config/load.go` — env-константа + парсинг.
- `cmd/rooms/main.go` — проброс `cfg.Transport.PeerCloseDrainTimeout`.

### Tests (зелёная)

- `tests/testutil/env/env.go` — wiring под новую сигнатуру фабрики.
- `tests/adapter/realtime/` — новый `peer_drain_test.go` (или расширение
  `peer_stop_test.go`): mock conn + gomock `InOrder` —
  (а) кадр, закоммиченный до `Stop`, отправлен до `Close`;
  (б) `drainTimeout=0` → `Stop` не ждёт;
  (в) заблокированный `Send` → `Stop` возвращается ≈за дедлайн
  (force-close прерывает write).
- `tests/adapter/realtime/peer_factory_test.go` — новая сигнатура.
- `tests/adapter/config/` — `t.Setenv` парсинг
  `TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT` (валидное, `0`, мусор → ошибка).
- `tests/integration/authoritative_host_test.go` — **без правок**,
  становится детерминированным; прогнать `-count=20` (см. Verification).

### Docs (зелёная)

- `internal/adapter/config/README.md` — строка env в таблице.
- `internal/adapter/transport/README.md` — knob + семантика `0`.
- `internal/adapter/realtime/README.md` — контракт `Stop`/drain.
- `GLOSSARY.md` — термин «Peer drain» (flush `outbound` до close,
  bounded; отличать от grace window reservation).
- `docs/adr/0012-peer-drain-close.md` — НОВЫЙ: решение, граница гонки,
  альтернативы (ack-канал, узкий API, оставить best-effort).
- `docs/client-protocol/errors.md` / `enet.md` — у `0x82`: доставка
  гарантирована до close (P30), не «best-effort».
- `docs/plans/P30-peer-drain-close.md` — этот файл.
- `docs/adr/0011` — **не трогаем** (история); ADR 0012 ссылается на него.

### Не трогаем

- `*_cgo.go` / tagged-файлы ENet — поведение меняется только через
  `Peer.Stop` выше по стеку.
- Codec, wire-layout — кадры не меняются.
- `reservation`, `admission`, delivery-слои — drain уровень `Peer`.

## Открытые детали для реализации

- `writeDone` vs вторая `sync.WaitGroup` — канал проще (`close` однажды
  в `defer` write-loop'а); выбор — косметика.
- Sweep-повтор в drain-фазе после наблюдения пустоты — не нужен:
  граница гонки зафиксирована контрактом.
- Новый файл теста vs расширение `peer_stop_test.go` — по объёму.

## Verification

1. `go build ./...`
2. `~/go/bin/golangci-lint run ./...`
3. `go test -race -shuffle=on -short -count=1 ./tests/...`
4. Детерминизм: `go test -race -count=20 -run AuthoritativeHost
   ./tests/integration/...` — без флаков (ранее опирался на Gosched).
5. `./scripts/enet-verify.sh build` + `vet` — tagged-код не правим, но
  `peer.go` на общем пути: подтвердить, что сборка под `-tags enet`
  не сломана.
6. CI-эквивалент `go test -race -shuffle=on -count=1 ./tests/...` перед
  пушем.
