# P23 — lifecycle dedup authority (runLeaveLocked !present → sentinel)

## Контекст

Аудит P20: дедуп двойного teardown (idle sweep ∥ transport teardown)
сегодня работает **неявно** — проигравший `runLeaveLocked` видит
`!present`, всё равно вызывает `policy.OnLeave(peer)`, и только то,
что `StateRoomPolicy.OnLeave` для отсутствующей state-записи возвращает
`ErrPeerNotFound`, гасит второй `OpPeerDisconnected`. Другая policy
(или регрессия в этой) → двойной broadcast / ложный второй OnLeave.

Загриллено: дедуп авторитетен на уровне реестра, не зависит от policy.

## Решение

`runLeaveLocked`: для `!present` сразу `return
fmt.Errorf("%w: %d", domain.ErrPeerNotFound, peerID)` — до delete,
без вызова `policy.OnLeave`, без метрик (метрик-гард `present`
остаётся для ясности веток).

- Тот же sentinel, что и public pre-check `Room.Leave` — семантика
  «peer не в комнате на момент применения» теперь едина на обоих
  уровнях.
- Проигравший `DisconnectPeer` получает `ErrPeerNotFound` → ветка
  «peer already left» → early return: одиночный broadcast гарантирован
  реестром, а не деталью StateRoomPolicy.
- Потеря: повторный OnLeave иногда дочищал state-запись (если первый
  OnLeave упал до delete) — покрывается prune-тиком
  `refreshPeerPings`, принято.
- `Replace`: leave-диспатч по `old`, вытесненному/удалённому до
  сериализации → `ErrPeerNotFound` → `old.Stop()` + `ErrReplaceFailed`
  (семантика не меняется — раньше туда же приводил policy-ответ).

## Скоуп правок (фиксированный)

- `internal/adapter/realtime/room_lifecycle.go` — ветка `!present`.
- `tests/adapter/realtime/` — тест: повторный leave того же peer-
  объекта → `ErrPeerNotFound`, `policy.OnLeave` не вызван второй раз
  (spy/capture-стаб по паттерну существующих), `peer_leaves`/`active_peers`
  без двойного декремента.
- `internal/adapter/realtime/README.md` — строка про dedup-инвариант,
  если раздел lifecycle её уже описывает.

## Верификация (для сабагента)

- `go build ./...` — 0 ошибок.
- `go vet ./...` — 0.
- `~/go/bin/golangci-lint run ./...` — 0 issues.
- `go test -race -shuffle=on -short -count=1 ./tests/...` — зелёные;
  новый тест в `tests/adapter/realtime/` падает до правки (red→green
  по желанию) и зелёный после.
