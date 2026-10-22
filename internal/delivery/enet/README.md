# delivery/enet

UDP host (отдельный порт от Fiber). Build tag `enet` + CGO.

**Клиентская спека (Godot, основной транспорт):** [docs/client-protocol/enet.md](../../../docs/client-protocol/enet.md).

## По умолчанию (`DefaultEnetConfig`)

Тип конфига — `transport/enet.EnetConfig` (`internal/adapter/transport/enet/enet_config.go`, без build-тегов); handler принимает его в `NewRoomsHandler`.

| Параметр | Значение | Env |
|----------|----------|-----|
| Порт | `7777` | `ENET_LISTEN_PORT` |
| Peer limit | `64` | `ENET_PEER_LIMIT` |
| Channel limit | `2` | — |
| Service timeout | `10ms` | — |
| Admit deadline (EventConnect → ticket) | `10s` | `ENET_ADMIT_TIMEOUT` |
| Первый пакет | сырой ticket (`admit`), max `TRANSPORT_MAX_INCOMING_FRAME_BYTES` | — |
| После admit | channel `0`, `[OpCode u8][payload]`; server send reliable | — |
| Inbound delivery | `PacketFlagReliable` → `DeliveryReliable`; без флага → `DeliveryUnreliable` (`flags_cgo.go`) | — |
| Oversized packet/frame | graceful disconnect (+ `LeaveRoom` если уже admitted) | — |
| Ошибка admit | OpCode `0x40`–`0x5F` и `0x70`–`0x7F` (1 байт) → **доставляется**, затем disconnect | — |
| Admit timeout | peer не прислал ticket за `AdmitTimeout` → disconnect без wire-кадра | — |

`EnetConfig.Validate()` — порт/лимиты/очереди/`AdmitTimeout`; env `ENET_*` — `adapter/config.LoadFromEnv()` → `cfg.Enet` (`MaxIncomingFrameBytes`/`EnetIncomingQueueSize` cmd/rooms копирует из `cfg.Transport`).

## Жизненный цикл listener'а

`RoomsHandler.Listen() *ListenHandle`: `Done()` — выход host loop, `Err()` — терминальная ошибка (bind failure), `Stop()` — отмена ctx + ожидание реального выхода. `main.go` фейлит процесс при ошибке bind.

Graceful shutdown: по ctx/Stop host loop закрывает все сессии (`conn.Close` → `peer.DisconnectLater` → `closed` chan будит Receive-горутины peer'ов), делает drain service-итерациями и `host.Destroy()`.

Disconnect везде graceful (`DisconnectLater` в `EnetConnection.Close`): queued reliable-пакеты — включая join reject — уходят до разрыва. `DisconnectNow` не используется.

## Сериализация libenet

C API libenet не thread-safe; host loop и peer outbound-горутины зовут его конкурентно. Все вызовы, трогающие C-состояние (`Host`/`Peer`/`Packet`, `NewHost`, `Initialize`), идут через `adapter/transport/enet.Call/Do` — один mutex на процесс (`connection_cgo.go`). Чтения `Event`-аксессоров (`GetType`/`GetPeer`/`GetPacket`/`GetData`) работают с Go-копией и лока не требуют. `sessions`-map трогает только host loop — отдельной синхронизации нет.

Без `enet`/cgo: `Listen()` — stub run (warn + чистый выход, `Err() == nil`).

```bash
CGO_ENABLED=1 go build -tags enet -o bin/rooms-enet ./cmd/rooms
CGO_ENABLED=1 go test -tags enet ./tests/delivery/enet/...
```

Поток join/leave — тот же `JoinRoomUseCase` / `LeaveRoomUseCase`, что WS.
