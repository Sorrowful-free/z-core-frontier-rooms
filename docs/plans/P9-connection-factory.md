# P9 — connection factory unification (ENet)

Скоуп зафиксирован гриллингом.

## Дизайн

- Единый seam — `realtime.PeerFactory` (`CreatePeer(ctx, id, nick, transport.Connection, room, logger)`): сервис уже работает с обоими типами соединений, фабрики соединений — транспортные детали delivery.
- Factory-интерфейсы остаются в `port/transport/{enet,ws}` (DI в handler'ах, тестируемость).
- `WsConnectionFactory` и `WsConnectionWaiter` — без изменений (асимметрия осознанна: у ENet read-поток принадлежит host-loop, у WS — connection waiter).

## Изменения (ENet)

- `internal/port/transport/enet/peer_cgo.go` — `type Peer = libenet.Peer`
- `internal/port/transport/enet/peer_stub.go` — `type Peer = any`
- `internal/port/transport/enet/connection_factory.go` — один untagged файл, единая сигнатура:
  `CreateConnection(peer Peer, incoming chan domain.Frame) transport.Connection`
  (заменяет `connection_factory_cgo.go` + `connection_factory_stub.go` в port)
- `internal/adapter/transport/enet/connection_factory_cgo.go` — реализация под ту же сигнатуру (alias = libenet.Peer, типобезопасно)
- `internal/adapter/transport/enet/connection_factory_stub.go` — та же сигнатура, аргументы игнорируются
- `internal/delivery/enet/session_cgo.go` — вызов без изменений (libenet.Peer = Peer под cgo)
- `internal/adapter/transport/README.md`, `internal/delivery/enet/README.md` — seam = PeerFactory, фабрики — транспортная деталь; tagged alias как паттерн
- `tests/` — compile-тесты обеих теговых веток фабрики

## Не трогаем

- `internal/adapter/transport/ws/**`, `internal/port/transport/ws/**`
- `internal/port/realtime/**` — seam уже на месте
- `internal/delivery/ws/**`
