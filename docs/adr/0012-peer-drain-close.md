# Peer drain-aware close: Send до Close вместо best-effort

ADR 0011 (Consequences) зафиксировал дыру: `peer.Stop` делал `cancel` → `conn.Close` → `wg.Wait`, а write-loop выходил по `ctx.Done` даже с непустым `outbound` — select-недетерминизм, последние кадры очереди терялись. На WS `0x82 OpRoomClosed` доставлялся best-effort: `closeRoom` компенсировал гонку `runtime.Gosched()` — приёмом, не гарантией. Дыра шире `0x82`: любой teardown (`closeRoom`, `evict`, `leave`, `kick`) мог потерять хвост `outbound`; на ENet «жёсткий порядок» `SendBytes` → `DisconnectLater` тоже держался только на удачном dequeue до `ctx.Done`.

## Decision

- **Drain внутри `Peer.Stop`** — универсально для всех teardown-путей, не узкий API «последний кадр». `Peer` получил `drainTimeout` и `writeDone chan struct{}` (замыкается write-loop'ом в `defer` при выходе).
- **Write-loop:** по `<-ctx.Done()` переходит в drain-фазу — дочитывает `outbound` до пустоты (`select`+`default`), затем возвращается. Ошибка `Send` в drain-фазе — conn мёртв, общий путь `conn.Close` + выход. `outbound` не закрывается (ADR 0003: продюсеры вне `p.wg`).
- **Stop:** `cancel` → если `drainTimeout > 0` **и peer был `Start`'нут** (`started atomic.Bool`), ждёт `writeDone` или дедлайн `time.NewTimer(drainTimeout)` → `conn.Close` → `wg.Wait`. Отдельный `writeDone` обязателен: ждать весь `wg` до `conn.Close` нельзя — read-loop висит в `Receive()` до close (deadlock). Дедлайн → force `conn.Close`, прерывающий даже заблокированный `Send` (WS write deadline 10s не растягивает `Stop`). `Stop` без `Start` безопасен: write-loop не запущен, `writeDone` не замкнётся никогда — `started`-флаг пропускает drain-ожидание.
- **Гарантия = «`Send` вызван до `Close`»**, не «клиент получил»: после нормального close() ядро досылает буфер; на ENet `SendBytes` гарантированно попадает в FIFO host loop'а до `DisconnectLater`.
- **Граница гонки — контракт порта:** кадры, чей `Deliver` закоммитил до начала `Stop`, получают попытку `Send` в пределах `drainTimeout`. `Deliver`, гонящийся со `Stop` (прошёл ctx-проверку, но enqueue после наблюдения пустой очереди write-loop'ом), — **unspecified**, кадр может быть потерян: «последний кадр» семантически тот, что закоммичен до teardown. `tryDeliver` перепроверяет `ctx.Err()` после enqueue — гонка со `Stop` возвращает «peer stopped», а не ложный `nil` за кадр, который может не уйти.
- **Конфиг:** `TRANSPORT_PEER_CLOSE_DRAIN_TIMEOUT` → `TransportConfig.PeerCloseDrainTimeout`, default `1s`, `Validate() >= 0`; `0` — drain выключен (семантика «механизм off», как `PeerIdleTimeout <= 0`). `NewPeer` без фабрики — пакетный дефолт 1s, фабрика вливает значение полем (сигнатура стабильна для white-box тестов, как `metrics`).
- **`closeRoom`:** `runtime.Gosched()` удалён — порядок «0x82 до close» теперь гарантирует drain-aware `peer.Stop`; порядок шагов (Deliver → DeleteRoom → UnregisterRoom → Stop) сохранён ради других инвариантов (peer ctx от `room.Context()`, transport-колбэк → `ErrRoomNotFound`). `Stop`'ы peers идут **параллельно**: серийный цикл дал бы O(N×drainTimeout) teardown на заблокированных conn (capacity до 127).
- **`room.Leave`:** `ErrRoomShouldClose` добавлен в sentinel-фильтр рядом с `ErrPeerNotFound`/`ErrPeerStale` — это заказанный policy сигнал на teardown (usecase пишет своё `Info "room closed by policy"`), Error-лог был ложным.

## Considered Options

- **Ack-канал на кадр** (`Deliver` ждёт подтверждения физического Send) — отклонено: превращало бы fire-and-forget fan-out в синхронный круг trip до conn на каждый кадр, а «последнему» кадру всё равно нужен teardown-семантика — сложность не окупается.
- **Узкий `DeliverFinal`/flush-API** для `closeRoom` — отклонено: лечит только путь `0x82`, оставляя потерю хвоста `outbound` на evict/leave/kick; новый метод порта ради одного сценария.
- **Оставить best-effort** (`Gosched` в usecase) — отклонено: не гарантия, потенциальный флак `TestIntegration_AuthoritativeHost_RoomDiesWithHost` под нагрузкой/CI; ADR 0011 прямо назвал drain-aware close отдельной работой.
- **Закрыть `outbound` как сигнал drain** — отклонено: противоречит ADR 0003 (продюсеры `tryDeliver` вне `p.wg` → send-on-closed TOCTOU); consumer выходит по `ctx.Done`, не по закрытию канала.
- **WaitGroup на write-loop вместо `writeDone`** — отклонено: равносильно по смыслу, канал проще (`close` однажды в `defer`); косметика.

## Consequences

- Все teardown-пути получают «Send закоммиченного хвоста до Close» без правок вызывающих: `closeRoom` (0x82), idle evict, `Leave`/`Replace`, shutdown.
- На ENet порядок `SendBytes` → `DisconnectLater` становится строгим де-юре (обе команды в одной FIFO host loop'а), а не «в практике»; на WS гарантия ограничена попыткой `Send` до `Close` — досыл TCP-буфера на ядре.
- `TestIntegration_AuthoritativeHost_RoomDiesWithHost` перестал опираться на scheduling yield — детерминирован.
- Стоимость: `Stop` может ждать до `drainTimeout` на заблокированном/медленном conn — bounded и opt-out (`0`); массовый teardown в `closeRoom` параллелен — O(1×timeout), не O(N).
- Клиентская обязанность обработать `0x82` неизменна (ADR 0011); гарантия касается только отправки, не доставки через сеть.
