# P22 — ENet reconnect e2e

## Контекст

План P20 («Верификация») требует e2e «разрыв → reconnect → resync»;
покрыта только WS-интеграция (`tests/integration/reconnect_test.go`).
ENet-ветка reconnect (первый пакет = token, `sess.session` teardown)
проверяется лишь косвенно. Загриллен полный скоуп: basic + expiry +
strict binding. Tagged (`enet && cgo`) — верификация через
`./scripts/enet-verify.sh`, CI — отдельная job по правилу AGENTS.md.

**Зависимость:** стартовать после P24 — ожидаемый reject-код на
просроченной grace зависит от enforce дедлайна (см. b).

## Тесты (загриллено: a+b+c)

### a) `TestE2E_ENetReconnect` — базовый сценарий

connect (join-тикет v3, admit) → `Disconnect` второго клиента →
peer A получает `0x80 OpPeerDisconnected{peerID u64}` → resume
первым пакетом с `reconnect_token` (v4) → admit успешен →
peer A получает `0x81 OpPeerReconnected` → вернувшийся получает
full state (resync), nick — из слота (не из токена).

### b) `TestE2E_ENetReconnectExpired` — grace-expiry

env с коротким `ReconnectTTL` (и sweep включённым): disconnect →
выждать past TTL → resume → reject. Ожидаемый код после P24:
`0x74 OpReservationExpired` (дедлайн задеплоен до refresh'а — reject
даже до прохода sweep); если слот уже swept → `0x73
OpReservationNotReserved`. Тест фиксирует обе ветки или детерминирует
одну (предпочтительно 0x74 через enforce).

### c) `TestE2E_ENetReconnectWrongHost` — strict binding v4

env с `ENET_LISTEN_PORTS` из 2+ портов (multi-host): тикет привязан к
host A (claims.hostID), resume-пакет на порт host B → reject
`0x77 OpWrongHost`, слот не тратится. Единственное e2e-покрытие
наследования hostID в v4.

## Скоуп правок (фиксированный)

- `tests/integration/reconnect_e2e_enet_test.go` — новый tagged файл
  (`//go:build enet && cgo`), клиентская инфраструктура — реюз из
  `match_e2e_enet_test.go` / `tests/testutil/env`.
- `tests/testutil/env` — при необходимости хелперы: dial-enet с
  произвольным токеном, env с N портами, `ReconnectTTL` в опциях env.
- `.github/workflows/test.yml` — новая job по паттерну `e2e-enet`
  (отдельный статус, свой timeout), в `enet-verify.sh` — включение
  файла в tagged-прогон, если он не подхватывается паттерном.

## Не в скоупе

- Scaffold `nil` admitPeerUseCase в `tests/delivery/enet/e2e_enet_test.go`
  — осознанный (покрывает пути до admit), задокументирован в коде.

## Верификация (для сабагента)

- `go build ./...` — 0 ошибок.
- `go vet ./tests/integration/` — 0.
- `./scripts/enet-verify.sh build` и `./scripts/enet-verify.sh vet` —
  зелёные (tagged-файл обязателен к проверке через Docker).
- `./scripts/enet-verify.sh test` (или целевой `go test -tags enet`
  по env-окружению) — три новых теста зелёные, `-race -count=1`
  без флаков на 3+ прогонах там, где возможно.
