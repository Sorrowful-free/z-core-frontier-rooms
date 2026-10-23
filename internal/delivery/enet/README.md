# delivery/enet

UDP host (отдельный порт от Fiber). Build tag `enet` + CGO.

**Клиентская спека (Godot, основной транспорт):** [docs/client-protocol/enet.md](../../../docs/client-protocol/enet.md).

## По умолчанию (`DefaultEnetConfig`)

Тип конфига — `transport/enet.EnetConfig` (`internal/adapter/transport/enet/enet_config.go`, без build-тегов); handler принимает его в `NewRoomsHandler`.

| Параметр | Значение | Env |
|----------|----------|-----|
| Порты | `[7777]` (CSV - N host'ов/shard'ов) | `ENET_LISTEN_PORTS` (alias `ENET_LISTEN_PORT`, побеждает) |
| Peer limit | `64` **на host** | `ENET_PEER_LIMIT` |
| Channel limit | `2` | — |
| Service timeout | `10ms` | — |
| Admit deadline (EventConnect → ticket) | `10s` | `ENET_ADMIT_TIMEOUT` |
| Первый пакет | сырой ticket (`admit`), max `TRANSPORT_MAX_INCOMING_FRAME_BYTES` | — |
| После admit | channel `0`, `[OpCode u8][payload]`; server send reliable | — |
| Inbound delivery | `PacketFlagReliable` → `DeliveryReliable`; без флага → `DeliveryUnreliable` (`flags_cgo.go`) | — |
| Oversized packet/frame | graceful disconnect (+ `LeaveRoom` если уже admitted) | — |
| Ошибка admit | OpCode `0x40`–`0x5F` и `0x70`–`0x7F` (1 байт) → **доставляется**, затем disconnect | — |
| Admit timeout | peer не прислал ticket за `AdmitTimeout` → disconnect без wire-кадра | — |

`EnetConfig.Validate()` - порты (1–65535, без дублей, ≥1)/лимиты/очереди/`AdmitTimeout`; env `ENET_*` - `adapter/config.LoadFromEnv()` → `cfg.Enet` (`MaxIncomingFrameBytes`/`EnetIncomingQueueSize` cmd/rooms копирует из `cfg.Transport`).

## Host sharding (P18)

На каждый порт из `cfg.ListenPorts` - отдельный `enetHost` (`id` = `domain.HostID` = индекс+1) со своим `HostLoop` и run-горутиной. Комната транспорт-агностична: её peer'ы могут сидеть на разных host'ах.

- **Балансировка:** `RoomsHandler` реализует `port/transport/enet.HostRegistry` - `AssignHost` выбирает least-loaded host по живым peer'ам (`enetHost.peers` инкрементится на `EventConnect`, декрементится в `sess.close`), round-robin tie-break. `IssueTicketUseCase` кладёт `HostID` в claims и `enet_port` в HTTP-ответ.
- **Strict binding:** admit сравнивает `claims.HostID` с `host.id` до `JoinRoom` - на чужом host'е join-reject `ErrWrongHost` → `OpWrongHost` (`0x77`) + disconnect, слот reservation не тратится; `HostIDAny` (ticket v2 / выписан без registry) admit'ится на любом host'е.
- **Peer→host привязка:** у каждого host'а своя `EnetConnectionFactory` (`WithLoop` на его `HostLoop` - `EnetConnection.Ping`/операции peer'а диспатчатся в правильный executor).
- **Fail-fast:** первый упавший run (bind failure) отменяет общий ctx mux'а; `Err()` = первая ошибка.

## Жизненный цикл listener'а

`RoomsHandler.Listen() *ListenHandle`: `Done()` - выход **всех** host loop'ов, `Err()` - терминальная ошибка (bind failure), `Stop()` - отмена ctx + ожидание реального выхода каждого loop'а. `main.go` фейлит процесс при ошибке bind.

Graceful shutdown: по ctx/Stop host loop закрывает все сессии (`conn.Close` → `peer.DisconnectLater` → `closed` chan будит Receive-горутины peer'ов), делает drain service-итерациями и `host.Destroy()`.

Disconnect везде graceful (`DisconnectLater` в `EnetConnection.Close`): queued reliable-пакеты — включая join reject — уходят до разрыва. `DisconnectNow` не используется.

## Сериализация libenet

C API libenet не thread-safe: весь C API host'а сериализует его `HostLoop` (`adapter/transport/enet`, P15). `host.Service` идёт blocking-командой `Call` в loop, peer/packet-операции dispatch'а (`session_cgo`/`frame_cgo`) - `Call`/`Do` в тот же loop. Сам dispatch исполняется на run-горутине, не на loop-горутине: admit/leave путь (`JoinRoom`/`LeaveRoom` → `dispatchLifecycle`) синхронно ждёт room-горутину, а та в `OnJoin`/`OnLeave` зовёт `EnetConnection.Ping` - blocking-вызов в тот же loop; dispatch на executor'е замкнул бы cross-deadlock executor↔room. `conn.Send`/`Close` - async enqueue в очередь loop'а (FIFO, wire-порядок сохраняется). `Event`-аксессоры (`GetType`/`GetPeer`/`GetPacket`/`GetData`) работают с Go-копией и диспатча не требуют. `sessions`-map трогает только run-горутина - синхронизации нет. Shutdown: `Stop` → `closeSessions` (graceful `DisconnectLater` в очередь) → `drainHost` (2 service-итерации через `Call`) → `loop.Stop()` → drain → `host.Destroy`.

Без `enet`/cgo: `Listen()` — stub run (warn + чистый выход, `Err() == nil`).

## Connection factory — tagged alias `Peer`

`EnetConnectionFactory` (`port/transport/enet`, адаптер `adapter/transport/enet`) — транспортная деталь delivery: настоящий seam с сервисом — `realtime.PeerFactory`, принимающий готовый `transport.Connection`. Интерфейс фабрики один на все build-теги — `CreateConnection(peer Peer, incoming chan domain.Frame)`, где `Peer` — tagged type alias: под `enet && cgo` это `libenet.Peer` (вызов `CreateConnection(peer, incoming)` из `session_cgo.go` типобезопасен и не менялся), в stub-сборке — `any`.

```bash
CGO_ENABLED=1 go build -tags enet -o bin/rooms-enet ./cmd/rooms
CGO_ENABLED=1 go test -tags enet ./tests/delivery/enet/...
```

Поток join/leave — тот же `JoinRoomUseCase` / `LeaveRoomUseCase`, что WS.
