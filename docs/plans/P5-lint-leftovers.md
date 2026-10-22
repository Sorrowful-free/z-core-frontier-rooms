# P5 — Lint leftovers

> **Статус:** done (verified: build/vet/lint clean, go test -race -shuffle=on green).

## Контекст

`golangci-lint run` (новый `.golangci.yml`: defaults + asciicheck + usestdlibvars) после вычитания находок P1–P4 оставляет три непокрытых долга. Атомарный микро-план, в основном удаление мёртвого кода.

## Находки

1. **`SA4000` — дублирующееся условие с side-effect** (tests/adapter/httplimits/limits_test.go:52): `!limits.AllowHTTPCreate(ip) || !limits.AllowHTTPCreate(ip)` — намерение «два запроса проходят», но `||` с вызовом-эффектом читается как логика. Развернуть в два последовательных `if !` + `t.Fatal`.
2. **`S1005`** (internal/domain/state/map_state.go:80): `for k, _ := range` → `for k := range`.
3. **unused `writeParseError`** (internal/delivery/http/errors.go:43) — удалить или задействовать; по факту кода решить и отчитаться (если ошибочно не подключён — это баг, а не dead code).

## Решения (зафиксировано)

- Только вычитка долга, чтобы lint-джоба была зелёной после P1–P5; никакой переработки.
- `writeParseError`: сначала понять, задумана ли она как обработчик parse-ошибок; если задумана, но не вызывается — подключить, если нет — удалить.

## Скоуп правок (override красной зоны — только эти файлы)

- `internal/delivery/http/errors.go`
- `internal/domain/state/map_state.go`
- `tests/adapter/httplimits/limits_test.go`

## Не трогать (чужие планы)

- `internal/adapter/realtime/**` — P1/P2/P4
- `internal/adapter/transport/**`, `internal/delivery/enet/**`, `internal/delivery/errors/**` — P1/P3
- `internal/adapter/realtime/README.md` — P4

## Шаги

1. `limits_test.go:52` — развернуть в два проверочных `if` (поведение не меняется: два consume → pass).
2. `map_state.go:80` — `for k := range`.
3. `errors.go` — `writeParseError`: решить по факту (подключить или удалить), отчитаться.

## Тесты

- Без новых тестов — только правка существующего кейса в п.1.

## Docs

- Не требуются.

## Проверки

```bash
go build ./... && go vet ./... && gofmt -l .
~/go/bin/golangci-lint run ./...   # должно быть 0 issues после P1–P5
go test -shuffle=on -count=1 ./tests/...
```
