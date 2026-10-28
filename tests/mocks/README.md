# Mocks (mockgen)

Generation from interfaces in `internal/port/` via [go.uber.org/mock](https://github.com/uber-go/mock) (`mockgen`).

```bash
# from the module root
go generate ./tests/mocks/...
```

Required in `go.mod`: `tool go.uber.org/mock/mockgen` and `require go.uber.org/mock`.

`mock_*.go` files **are committed** to the repository so that `go test ./tests/...` works without installing mockgen.

Import package in tests:

```go
"github.com/Sorrowful-free/z-core-frontier-rooms/tests/mocks"
```

Use `gomock.Controller` and `mocks.NewMock…(ctrl)`.
