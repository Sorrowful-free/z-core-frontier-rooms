# План исправлений по ревью `z-core-frontier-rooms`

Хендофф-документ для агента-исполнителя. Содержит весь необходимый контекст —
исполнитель может не иметь доступа к исходной переписке ревью.

## Контекст

Go-сервис игровых комнат: control plane (HTTP, Fiber v3) + data plane (WS/ENet).
Слоистая архитектура: `domain → port ← adapter`, `delivery → usecase → port`.

### Обязательные правила проекта (соблюдать строго)

- Тесты — только в `tests/`, внешний пакет (`package room_test`), запуск `go test ./tests/...`.
  НЕ класть `*_test.go` рядом с production-кодом.
- HTTP — только Fiber v3 (`github.com/gofiber/fiber/v3`).
- При изменении поведения/контракта/публичного API пакета — обновить его `README.md`
  в том же PR (см. `.cursor/rules/package-docs.mdc`).
- Конфиг модуля — в `adapter/<module>/<module>_config.go` с `Validate()`; env-парсинг
  в `adapter/config/load*.go`; `adapter/<module>` НЕ импортирует `adapter/config`.
- Моки портов — mockgen в `tests/mocks/`, `go generate ./tests/mocks/...`.
- После реализации функционала спросить пользователя: «Нужно ли написать тесты?» —
  не писать тесты молча.
- Язык общения — русский.

Перед стартом зафиксировать зелёную базу: `go build ./... && go test ./tests/...`.

---

## Задача 1 (Критично) — паника «send on closed channel» в `Room`

**Файлы:** `internal/adapter/realtime/room.go` (+ `room_lifecycle.go`)

**Проблема.** `Room.Stop()` закрывает каналы `incoming` и `lifecycle`, но `r.wg` ждёт
только горутину `processRoomEvents`. Продюсеры каналов — горутины peers
(`processIncomingEvents → room.Deliver → tryDeliver`) и use cases (`dispatchLifecycle`)
— в `r.wg` не входят. `select` при готовом case «отправка в закрытый канал» паникует.
Сейчас спасает лишь внешний инвариант «peers останавливаются до `room.Stop()`»,
но `registry.DeleteRoom` зовёт `room.Stop()` напрямую без kick.

**Решение (рекомендуется A).**
- **A (минимальный, безопасный):** убрать `close(r.incoming)` и `close(r.lifecycle)`
  из `Stop()`. Консьюмер `processRoomEvents` завершается по `<-r.ctx.Done()`; каналы
  соберёт GC. Это убирает первопричину.
- **B (строже, только по согласованию):** сделать Room владельцем peers и
  останавливать их в `Stop()` до закрытия каналов. Расширяет ответственность Room —
  не делать без согласования.

**Acceptance.**
- В `Stop()` нет `close()` на каналы, в которые пишут внешние горутины.
- `go build ./...` зелёный; `go test -race ./tests/adapter/realtime/... ./tests/adapter/registry/...` проходят.
- Обновлён `internal/adapter/realtime/README.md`, если там описан порядок закрытия каналов.

---

## Задача 2 (Существенно) — валидация верхней границы `capacity`

**Файлы:** `internal/delivery/http/parse.go`, при необходимости `internal/domain/`.

**Проблема.** `validateCapacity` проверяет только `capacity <= 0`. `Room.GetCapacity()`
клампит до `int8` (макс 127), wire/state используют `int8`. При `capacity > 127`
реальная вместимость (Room/Reservation) = N, а в state анонсируется 127 → рассинхрон.

**Решение.**
- Ввести константу максимума (например `domain.MaxRoomCapacity = 127`), использовать
  её и в валидации, и в клампе `Room.GetCapacity()` (убрать магические 127).
- В `validateCapacity` вернуть ошибку при `capacity > MaxRoomCapacity` с кодом
  `codeInvalidCapacity` (уже используется в `rooms.go`).

**Acceptance.**
- `POST /rooms` с `capacity > 127` → `400` + `codeInvalidCapacity`.
- `Room.GetCapacity()` использует ту же константу.
- Обновлены `internal/delivery/http/README.md` и (если константа в domain) `internal/domain/README.md`.

---

## Задача 3 (Существенно) — явная семантика дропа в `Deliver`

**Файлы:** `internal/adapter/realtime/room.go`, `internal/adapter/realtime/peer.go`.

**Проблема.** `Room.Deliver`/`Peer.Deliver` всегда возвращают `nil`; дроп из-за
переполнения очереди неотличим от успешной доставки. Есть мёртвый код (повторный
`ctx.Err()` после неуспешного `tryDeliver`, всё равно `nil`). Молчаливый дроп
full-state тиков → необнаруживаемый рассинхрон.

**Решение.**
- Убрать мёртвый второй `ctx.Err()`-блок.
- Развести исходы: «остановлен» (ctx done) → ошибка; «очередь полна» → политика дропа
  (для input допустим; рассмотреть sentinel `domain.ErrQueueFull`, который вызывающий
  логирует, но не считает фатальным). Сохранить «не рвать соединение из-за переполнения».
- Для full-state — минимум отдельный лог/метрика; полноценный backpressure — отдельной
  задачей, не в этом PR без согласования.

**Acceptance.**
- Нет мёртвого кода; контракт `Deliver` описан в `internal/adapter/realtime/README.md`.
- Различимы исходы «остановлен» и «очередь полна» в коде и тестах.
- При изменении контракта синхронно обновить `tests/adapter/realtime/queue_drop_test.go`.

---

## Задача 4 (Существенно, оптимизация) — контеншен в `Reservation`

**Файл:** `internal/adapter/reservation/reservation.go`

**Проблема.** Один глобальный `sync.Mutex` сериализует Reserve/Admit/Revoke/State/List
по всем комнатам — глобальная точка контеншена на горячем пути.

**Решение (по согласованию).**
- Вариант 1: per-room lock — внешняя map под `RWMutex` только для поиска/создания/удаления
  комнаты, мутации слотов под мьютексом комнаты.
- Вариант 2: шардирование по `roomID`.
- Контракты порта `port/reservation` и доменные ошибки — без изменений.

**Acceptance.**
- Внешний API `Reservation` неизменен; `tests/adapter/reservation/...` зелёные под `-race`.
- Обновлён `internal/adapter/reservation/README.md` (модель блокировок).
- Желателен бенч до/после (после вопроса пользователю).

> Оптимизация. Делать после задач 1–3, отдельным PR.

---

## Задача 5 (Незначительно, быстрые победы) — конфиг и безопасность

Сгруппировать в один PR:

1. **Логгер прод-режима.** `cmd/rooms/main.go:42` — `zap.NewDevelopment()` → конфигурируемый
   (env, по умолчанию `NewProduction`). Env (например `LOG_MODE=prod|dev`) по правилам конфига.
2. **Адрес прослушивания.** `main.go:117` хардкод `:3000` → env `HTTP_ADDR` (дефолт `:3000`)
   через `LoadFromEnv`.
3. **Токен в query WS.** `internal/delivery/ws/rooms.go:54` — токен в URL попадает в access-логи
   (`fibzap` на весь app). Минимум: исключить query из логирования / перейти на заголовок
   или WS subprotocol. Изменение протокола согласовать (затрагивает клиентов).
4. **Порядок graceful shutdown.** `main.go:136-152` — рассмотреть `fiberApp.ShutdownWithContext`
   до чистки комнат, чтобы не принимать новые join во время teardown.

**Acceptance.** Новые env задокументированы в `internal/adapter/config/README.md` и
`.env.example`; `go build ./...` зелёный.

---

## Порядок выполнения и PR-структура

1. **PR1 — Задача 1** (критично, изолированно). Обязательно `-race`.
2. **PR2 — Задача 2** (capacity, маленький, низкий риск).
3. **PR3 — Задача 3** (контракт Deliver; согласовать политику дропа).
4. **PR4 — Задача 5** (конфиг/безопасность, пачкой).
5. **PR5 — Задача 4** (оптимизация Reservation, с бенчем).

Каждый PR маленький и ревьюабельный; не смешивать. После каждого:
`go build ./... && go test -race ./tests/...`, обновить затронутые `README.md`,
спросить пользователя про тесты перед их написанием.

## Definition of Done

- `go build ./...`, `go vet ./...`, `go test ./tests/...` (желательно `-race`) — зелёные.
- Линтеры проекта (если настроены) без новых замечаний.
- Затронутые пакетные `README.md` обновлены (`package-docs.mdc`).
- Изменения контрактов портов/доменных ошибок отражены в `delivery/http`, `delivery/errors`
  и чеклистах.
- Никаких изменений git-конфигурации; коммиты/PR — только по явной просьбе пользователя.
