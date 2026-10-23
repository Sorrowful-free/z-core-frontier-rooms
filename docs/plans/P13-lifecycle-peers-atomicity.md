# P13: атомарность lifecycle и peers-реестра комнаты

## Проблема

`Room.Join`/`Leave` выполняют `policy.OnJoin`/`OnLeave` через `dispatchLifecycle`
на room-loop'е (сериализовано с full/patch тиками), но мутацию `r.peers`
выполняет вызывающая горутина **после** возврата dispatch.

Окно гонки (Join):

```text
loop goroutine:  OnJoin → peer попал в policy state
                 ← patch tick успевает сюда →
caller goroutine:                          r.peers[id] = peer
```

`refreshPeerPings` на patch-тике видит peer в state, но `room.GetPeer` →
`ErrPeerNotFound` → peer помечается stale и **выкидывается из state**
(`pruning stale peer`). Новоприсоединившийся клиент теряет input/флаги до
переподключения; если он был мастером, срывается election.

Обнаружено агентом в P12 при прогоне ENet e2e: при дефолтном patch-интервале
50ms окно срабатывало регулярно (тестово замитигировано `WithRoomPatchInterval`,
это не фикс).

## Фикс

Перенести мутацию `r.peers` (и авторитетную capacity-проверку) внутрь
`runLifecycle`, чтобы insert/delete в peers и `OnJoin`/`OnLeave` выполнялись
атомарно относительно тиков на одном goroutine'е:

- `lifecycleJoin`: под `r.mutex` capacity-check + `peers[id]=peer` → `OnJoin`;
  при ошибке `OnJoin` — откат `delete(peers)`.
- `lifecycleLeave`: `delete(peers)` → `OnLeave`. При ошибке `OnLeave` peer уже
  вне room-реестра; state-запись само-вычищается следующим prune-тиком.
- `Join`: pre-check capacity под `RLock` (ранний ответ), dispatch, ошибка →
  `ErrJoinDenied`. Post-check больше не нужен: авторитетная проверка в loop'е.
- `Leave`: pre-check существования, dispatch.
- `Replace`: dispatch(leave old) → dispatch(join new); при ошибке join —
  rollback dispatch(join old). Без ручной мутации map'ы между dispatch'ами.

Deadlock-анализ: `runLifecycle` берёт `r.mutex` коротко на loop'е; ни один
caller не держит `r.mutex`, блокируясь на `r.lifecycle`/`req.done` — блокировки
не пересекаются.

## Скоуп правок

- `internal/adapter/realtime/room.go` (Join/Leave/Replace)
- `internal/adapter/realtime/room_lifecycle.go` (runLifecycle)
- `tests/adapter/realtime/` — регрессионный тест (spy-policy: внутри `OnJoin`
  `room.GetPeer(id)` обязан быть виден; внутри `OnLeave` — `ErrPeerNotFound`)
- `internal/adapter/realtime/README.md` — контракт порядка lifecycle
- этот файл

Не трогать: codec, transport, delivery, usecase, остальные планы.

## Верификация

- `go build ./... && go vet ./... && ~/go/bin/golangci-lint run ./...`
- `go test -race -shuffle=on -count=1 ./tests/...`
- `go test -race -run 'TestE2E_LiveMatch_HostMigration' -count=1 ./tests/integration/`
- `./scripts/enet-verify.sh test` — tagged прогон (ENet/mixed e2e)
- после фикса `WithRoomPatchInterval(1s)` в e2e можно вернуть к 50ms дефолту —
  окно закрыто (оставить как есть или вернуть: решить при реализации).
