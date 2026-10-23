# P14: консолидация master-election в единый переходный узел

## Проблема

Семантика выборов мастера сейчас размазана по файлам:

- `master_election.go` — `electMaster`/`assignMaster` (кандидаты, stale-skip,
  валидация GetPeer)
- `server_state_room_policy.go` — `masterPeerID`, post-commit `masterAssigned`
  (2 места: refreshPeerPings, prepareAndSendRoomStatePatch)
- `client_state_room_policy.go` — `masterPeerID`, lazy election на RPC,
  post-commit `masterAssigned`
- masterless-эпизод логирования — где-то среди них

Каждый триггер (tick, RPC, join, leave) знает про election и post-commit
самостоятельно. P8 фиксил именно дивергенцию этих путей; инвариант-харнес P10
теперь страхует, но структурная сложность осталась — любой новый триггер
обязан повторить весь ритуал (elect → assign → commit → masterAssigned).

## Цель

Полноценная state-machine с явными переходами/событиями (решение grill Q1,
вариант b): master-эпизоды как состояния (`masterSet`/`masterless` +
лог-дедупликация), события триггеров (`peerJoined/peerLeft/peerDied/tick/
rpc`) сводятся в один диспетчер переходов. Триггеры эмитят события, машина
решает election + IsMaster-флаги + masterless-лог в одном месте.

## Границы

- Чистый рефактор, **семантика идентична** (инвариант-харнес + master_recovery
  тесты — спецификация; должны пройти без правок)
- Только `internal/adapter/realtime/policy/state/*.go`
- Не трогать: триггерный интерфейс policy, wire-протокол, client policy поверх
  общих хелперов

## Верификация

- `go test -race -shuffle=on -count=1 ./tests/adapter/realtime/...` (инварианты +
  property 5 seeds должны пройти неизменными — это и есть спецификация)
- e2e на миграции мастера: WS + tagged ENet в Docker
