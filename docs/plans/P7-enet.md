# P7 — ENet-волна

**Статус:** done + review fixups (config канонизирован: тип в `adapter/transport/enet`, поле `Enet` в root `Config`, единый `LoadFromEnv`; standalone `LoadEnetFromEnv` и упоминания `delivery/enet.Config` ниже — исторически).

Отложенные находки аудита по `internal/{adapter/transport,delivery}/enet/**` — код под build-тегами `enet && cgo`, локально не компилировался. Теперь верификация возможна локально через `.devcontainer/Dockerfile` (образ `zcore-rooms-dev`, внутри `libenet-dev` + Go 1.25):

```bash
docker build --network=host -t zcore-rooms-dev -f .devcontainer/Dockerfile .devcontainer/
docker run --rm --network=host -v "$PWD":/work -w /work -e CGO_ENABLED=1 zcore-rooms-dev \
  go build -tags enet ./...
docker run --rm --network=host -v "$PWD":/work -w /work -e CGO_ENABLED=1 zcore-rooms-dev \
  go test -race -count=1 -tags enet ./tests/delivery/enet/...
```

## Находки

### Major

1. **Join-reject frame теряется** — `run_cgo.go` admit/malformed пути: `SendJoinReject` кладёт пакет в очередь, затем `sess.close()` → `peer.DisconnectNow(0)` ресетит peer и выбрасывает queued outgoing. Фикс: после постановки reject — graceful `peer.Disconnect()` (ENET гарантирует доставку pending reliable + шлёт disconnect после flush) или `host.Flush()` + отложенный disconnect.
2. **libenet из нескольких горутин** — C API не thread-safe; `EnetConnection.Send/DisconnectNow/GetRoundTripTime` вызываются с peer-outbound горутин и sweep'ов параллельно с host-циклом. Фикс: единая сериализация всех libenet-вызовов — mutex вокруг каждого вызова (выбран простой вариант; dispatch-to-host-loop — отдельный рефакторинг при необходимости).
3. **Unadmitted-сессии без дедлайна** — после `EventConnect` сессия занимает PeerLimit-слот бесконечно (idle-sweep покрывает только admitted peers). Фикс: connect→admit TTL — `time` в сессии, проверка в host-цикле (service с периодическим таймаутом), disconnect просроченных. Конфиг: `AdmitTimeout` в `delivery/enet.Config` (дефолт ~10s).
4. **Startup-фейл проглатывается** — `RoomsHandler.Listen()` спавнит горутину, ошибки `run` только логируются; при фейле bind процесс живёт без data plane. Фикс: `Listen()` возвращает handle (`Stop()`/error-канал) либо синхронно сигналит bind-результат; `main.go` реагирует.

### Minor

5. **Graceful shutdown** — `Listen()` без Stop-контракта; при ctx-cancel host-цикл выходит и `host.Destroy()` бежит, но сессии не `close()`'нутся → peer `Receive`-горутины висят до выхода процесса. Фикс: в shutdown-ветке закрывать все сессии (disconnect peers → `closed` chan будит `Receive`), Stop ждёт завершения.
6. **Мёртвый код** — `session_cgo.go` `clearPeerData`/`SetData(nil)` (peer data нигде не устанавливается); `mu sync.Mutex` в `run_cgo.go` охраняет map, трогаемую только host-loop горутиной — убрать.
7. **Config** — `delivery/enet/Config` без `Validate()` (нарушение config.mdc); `ListenPort`/`PeerLimit` не wired в env (`main.go` копирует только два поля). Фикс: `Validate()` + env `ENET_LISTEN_PORT`, `ENET_PEER_LIMIT`, `ENET_ADMIT_TIMEOUT` через `adapter/config` + README.

### Info / сознательно не трогаем

- `EnetConnectionFactory` разная сигнатура под тегами (cgo принимает `libenet.Peer`, stub — нет) — без глубокого рефакторинга порта не унифицировать; оставлено как есть, зафиксировано в README.
- `IsAlive` только по `Close` — протокольный таймаут ENet — осознанно.
- Port зависит от vendor-типов (`libenet.Peer`, `*websocket.Conn`) — отдельный вопрос дизайна портов, не в этой волне.

## Файлы (зафиксированный скоуп)

- `internal/delivery/enet/{run_cgo.go, session_cgo.go, room_handler.go, config.go, frame_cgo.go, README.md}`
- `internal/adapter/transport/enet/{connection_cgo.go, connection_factory_cgo.go, flags_cgo.go}` — только если нужно для сериализации вызовов
- `internal/adapter/config/{load.go, README.md}` — env wiring
- `cmd/rooms/main.go` — startup-пропагация ошибки ENet
- `tests/delivery/enet/**` — новые/обновлённые тесты под `-tags enet`
- `docs/client-protocol/enet.md` — если меняется наблюдаемое поведение (reject теперь доезжает, admit timeout)

## Не трогать

- `internal/delivery/ws/**`, `internal/adapter/realtime/**`, usecase — вне скоупа
- `port/transport` сигнатуры — без редизайна в этой волне

## Верификация

- Хост: `go build ./...`, `go vet ./...`, `golangci-lint run ./...`, `go test -race -shuffle=on -count=1 ./tests/...` (stub-ветки не должны регрессировать)
- Docker: `go build -tags enet ./...`, `go vet -tags enet ./internal/... ./cmd/...`, `go test -race -count=1 -tags enet ./tests/delivery/enet/...`
- Docs: README пакетов + `docs/client-protocol/enet.md` по факту изменений
