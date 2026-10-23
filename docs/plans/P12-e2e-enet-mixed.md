# P12 — e2e live match: ENet + mixed transports

Зелёная зона: `tests/integration/`, `tests/testutil/env/`, `.github/workflows/test.yml`,
`scripts/enet-verify.sh`, `tests/README.md`. Production не трогаем.

## Цель

Live-match сценарий P11 на двух конфигурациях транспорта:

1. **`TestE2E_LiveMatch_ENet`** — 4 ENet-клиента; дроп хоста = честное
   молчание libenet (клиент перестаёт `host.Service` → нет ack → серверный
   libenet-таймаут ~5–30s → EventDisconnect → leave → миграция)
2. **`TestE2E_LiveMatch_Mixed`** — 2 WS + 2 ENet в одной комнате;
   host на ENet (таймаут-дроп — новое покрытие), WS-выжившие наблюдают
   миграцию через общий wire-протокол

## Дизайн (зафиксирован гриллингом)

- **Честный libenet-таймаут**: ENet host-клиент замолкает (стоп Service,
  UDP-сокет жив) → сервер детектирует за ~5–30s → миграция. Ожидание эвикта
  до ~45s, сценарий ~90–120s реального времени
- **Общий клиентский слой**: интерфейс/структура `matchClient` — readPump,
  sendLoop, statsLoop, наблюдения (masterSeq/entityOwners/inputNicks/
  shadow RoomState). Две реализации: `wsClient` (есть) и `enetClient`
  (новый, по паттерну `enetTestClient` из tests/delivery/enet)
- **Сценарий один**, параметр — фабрика клиентов: общий `runLiveMatch(t,
  mkClients)` двигает ту же хронологию (join → стрим → дроп хоста →
  миграция → clean leave мастера-2 → финал ≥2 живых)
- **`WithEnet(cfg)`** в `tests/testutil/env` — tagged-файл
  `env_enet.go` (`enet && cgo`): wiring `delivery/enet` handler на
  свободном UDP-порту (продовый `Listen`/`Stop`, cleanup в t.Cleanup),
  `e.EnetAddr()` для клиентов
- **Все ENet-e2e файлы** — `//go:build enet && cgo`: клиент, сценарии,
  env-опция; без тега пакет компилируется как раньше (WS e2e untagged)

## Фактура (проверено)

- ENet admit: первый пакет = сырой ticket token → `JoinRoom`
- Кадры после admit идентичны WS: `OpCode + payload`, тот же протокол
- `apiMu` общий: клиентские libenet-вызовы тоже через `adapterenet.Call/Do`
- ENet `IsAlive` у conn — alive flag; «зомби» вычищает libenet-таймаут
  транспорта (EventDisconnect → LeaveRoom), не наш sweep

## Файлы

- `tests/testutil/env/env_enet.go` — `WithEnet` + `EnetAddr` (tagged)
- `tests/integration/match_e2e_client.go` — общий интерфейс + сценарный
  драйвер (untagged, wsClient остаётся)
- `tests/integration/match_e2e_enet_test.go` — `enetClient` +
  `TestE2E_LiveMatch_ENet` + `TestE2E_LiveMatch_Mixed` (tagged)
- `.github/workflows/test.yml` — **каждый e2e сценарий = отдельная джоба**
  (параллельно, честные статусы): `e2e-ws` (untagged), `e2e-enet` и
  `e2e-mixed` (обе: libenet-dev, `CGO_ENABLED=1`, `-tags enet`,
  `-run '^<ИмяТеста>$'`, `-timeout 300s`)
- `scripts/enet-verify.sh` — `test` таргет включает `./tests/integration`
- `tests/README.md` — строки + как запускать

## Ассерты (те же, что P11, плюс)

- slaves видят entities от ≥2 distinct мастеров (по Owner) на обоих транспортах
- input каждого клиента виден остальным — включая кросс-транспорт
  (WS-клиент видит input ENet-клиента и наоборот)
- две миграции `is_master` на wire; финал ≥2 живых, patches текут
- логи `[nick]`/`[scenario]` общий формат

## Критерий выхода

- `./scripts/enet-verify.sh test` — ENet+mixed e2e зелёные в Docker
- `go test -race -shuffle=on -count=1 ./tests/...` (без тега) — зелёный
