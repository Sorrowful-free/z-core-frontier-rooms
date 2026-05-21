# z-core-frontier-rooms

Сервис игровых комнат (HTTP API на [Fiber](https://gofiber.io/), WebSocket, ENet).

## Требования

- Go 1.25+

## Запуск

```bash
go run ./cmd/rooms
```

## Сборка

```bash
go build -o bin/rooms ./cmd/rooms
```

## Тесты

Тесты — в каталоге [`tests/`](tests/), не рядом с production-кодом.

```bash
go test ./tests/...
```
