# Моки (mockgen)

Генерация из интерфейсов в `internal/port/` через [go.uber.org/mock](https://github.com/uber-go/mock) (`mockgen`).

```bash
# из корня модуля
go generate ./tests/mocks/...
```

Требуется в `go.mod`: `tool go.uber.org/mock/mockgen` и `require go.uber.org/mock`.

Файлы `mock_*.go` **коммитятся** в репозиторий, чтобы `go test ./tests/...` работал без установки mockgen.

Пакет импорта в тестах:

```go
"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
```

Используйте `gomock.Controller` и `mocks.NewMock…(ctrl)`.
