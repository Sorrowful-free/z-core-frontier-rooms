# P33 — master-migration как глобальный env-флаг (разворот P29)

## Контекст

Разворот ADR-0011: per-room `policy` kind заменяется глобальным флагом
деплоя. `StateRoomPolicy` получает `allowMasterMigration`: `false` → уход
master'а отвечает `ErrRoomShouldClose` (тот же teardown, что был у
authoritative-host: `OpRoomClosed 0x82` → unregister → revoke → disconnect).
Весь kind-механизм (`KindRoomFactory`, `AuthoritativeHostPolicy`,
`RoomPolicyKind`, `policyKind`-параметр сквозь port/usecase/HTTP)
удаляется чисто. ADR-0011 уже переписан под новое решение.

Загрилленные решения (уже согласованы):

1. Семантика `false` = ex-authoritative-host (`ErrRoomShouldClose` на
   входе `OnLeave`, до master-машины и staged-мутаций).
2. `policy` из `POST /rooms` убирается; неизвестное поле в body
   игнорируется — ок.
3. Env `STATE_POLICY_ALLOW_MASTER_MIGRATION`, дефолт `true`; module
   config `state.StatePolicyConfig{AllowMasterMigration bool}` +
   `Validate()` (тривиальный, для единообразия), поле
   `Config.StatePolicy`, `loadStatePolicyFromEnv()`.
4. Флаг вливается **полем** фабрикой (прецедент `p.master.metrics`) —
   сигнатура `NewStateRoomPolicy` не меняется;
   `NewStateRoomPolicyFactory(logger, cfg StatePolicyConfig, m)` принимает
   конфиг конструктором.
5. ADR-0011 правлен на месте (сделано), новых ADR нет.

## Скоуп правок (зафиксирован)

### Прод-код

- `internal/adapter/realtime/policy/state/state_room_policy.go` — поле
  `allowMasterMigration bool`; в `OnLeave` до
  `prepareAndSendRoomStatePatch`:
  `if !p.allowMasterMigration && p.master.isMaster(peer.GetID()) → ErrRoomShouldClose`.
- `internal/adapter/realtime/policy/state/state_room_policy_factory.go` —
  `NewStateRoomPolicyFactory(logger, cfg StatePolicyConfig, m)`; в
  `CreateRoomPolicy` полевой влейв `p.allowMasterMigration`.
- **Новый** `internal/adapter/realtime/policy/state/state_policy_config.go` —
  `StatePolicyConfig` + `Validate()`.
- `internal/adapter/realtime/room_factory.go` — `CreateRoom` без
  `policyKind`; правка комментария.
- `internal/adapter/registry/room_registry.go` — `CreateRoom` без
  `policyKind`.
- `internal/port/realtime/room_factory.go` — сигнатура без `policyKind`.
- `internal/port/registry/room_registry.go` — сигнатура без `policyKind`.
- `internal/usecase/room/create.go` — `Create` без `policyKind`.
- `internal/delivery/http/dto.go` — поле `Policy` из `createRoomRequest`.
- `internal/delivery/http/rooms.go` — блок `ParseRoomPolicyKind` и аргумент.
- `internal/delivery/http/errors.go` — `codeInvalidPolicy` + case
  `ErrInvalidPolicy` в маппере.
- `internal/domain/room_errors.go` — удалить `ErrInvalidPolicy`
  (`ErrRoomShouldClose` остаётся!).
- `internal/adapter/config/config.go` — поле `StatePolicy
  statepolicy.StatePolicyConfig` (+ import alias).
- `internal/adapter/config/load.go` — env
  `STATE_POLICY_ALLOW_MASTER_MIGRATION`, `loadStatePolicyFromEnv()`
  (пусто → `true`; `ParseBool` для явных значений, мусор → ошибка),
  вызов в `LoadFromEnv`.
- `cmd/rooms/main.go` — один `NewRoomFactory` с
  `NewStateRoomPolicyFactory(zaplog…, cfg.StatePolicy, m)`; блок
  `KindRoomFactory` и импорты `domain`/`portrealtime` подчистить, если
  осиротеют.
- `internal/loadgen/config.go` — `PolicyMix`, `mapPolicy`,
  `LOADGEN_POLICY_MIX`.
- `internal/loadgen/fleet.go` — `plan.policy`, `body["policy"]`,
  `p.roomPolicy`; `internal/loadgen/peer.go` — поле `roomPolicy`.

### Удалить целиком

- `internal/adapter/realtime/kind_room_factory.go`
- `internal/adapter/realtime/policy/state/authoritative_host_policy.go`
- `internal/domain/room_policy.go`
- `tests/adapter/realtime/kind_room_factory_test.go`

### Тесты (переработка)

- `tests/testutil/env/env.go` — убрать `KindRoomFactory`-сборку; поле
  `statePolicy statepolicy.StatePolicyConfig` (дефолт
  `{AllowMasterMigration: true}`) + опция `WithStatePolicy(...)`;
  удалить `CreateRoomWithPolicy` (поле `policy` мертво).
- `tests/adapter/realtime/policy/state/authoritative_host_policy_test.go` —
  переписать под флаг: policy через
  `NewStateRoomPolicyFactory(logger, StatePolicyConfig{false}, m)`.
- `tests/integration/authoritative_host_test.go` — env с
  `WithStatePolicy({false})`, create без `policy`.
- `tests/delivery/http/rooms_test.go` — убрать `policy` из request-body
  и кейсы `invalid_policy`.
- `tests/usecase/room/create_test.go`, `tests/adapter/registry/*`,
  `tests/adapter/realtime/*` — механически убрать аргумент `policyKind`.
- `tests/mocks/` — регенерация mockgen (`MockRoomFactory`,
  `MockRoomRegistry`): `go generate ./tests/mocks/...`.
- **Новый** `tests/adapter/config/` — loader-тест с `t.Setenv` для
  `STATE_POLICY_ALLOW_MASTER_MIGRATION` (дефолт/явные/`"false"`/мусор).

### Документация (по факту правок)

- `internal/domain/README.md` — убрать строку `RoomPolicyKind`;
  `ErrRoomShouldClose` — поменять «P29» на актуальный триггер.
- `internal/adapter/realtime/policy/state/README.md` — убрать
  `AuthoritativeHostPolicyFactory` из текста и таблицы файлов; описать
  флаг + `StatePolicyConfig`.
- `internal/adapter/realtime/README.md` — если упоминает kind-dispatch.
- `internal/adapter/config/README.md` — env
  `STATE_POLICY_ALLOW_MASTER_MIGRATION`.
- `docs/client-protocol/http.md` — убрать строку `policy` и
  `invalid_policy` из ошибок create.
- `GLOSSARY.md` — термин «Authoritative host»: теперь режим деплоя
  (`STATE_POLICY_ALLOW_MASTER_MIGRATION=false`), не room policy.
- `.cursor/rules/config.mdc` — модуль `policy/state` в таблицу слоёв
  (новый `*Config`).
- Заголовок-памятку в этом файле и ADR-0011 оставить как есть.

### Не трогать

- `ErrRoomShouldClose`, `closeRoom`, `OpRoomClosed 0x82`, `masterMachine`,
  `port/realtime/policy` (`RoomPolicy`/`RoomPolicyFactory`), кодек/wire.
- Файлы под тегом `enet` — policyKind туда не протянут (дополнительная
  проверка в verification).

## Порядок

1. `StatePolicyConfig` + флаг в `StateRoomPolicy` + фабрика.
2. Удаление kind-цепочки снизу вверх: ports → registry → usecase →
   http → domain → factories → main.
3. Loadgen, testutil/env, переработка тестов, mockgen.
4. Docs по факту, loader-тест.

## Verification

- `go build ./...` — чисто.
- `go test -race -shuffle=on -short -count=1 ./tests/...` — зелёный.
- `~/go/bin/golangci-lint run ./...` — чисто.
- `./scripts/enet-verify.sh build && ./scripts/enet-verify.sh vet` —
  только если зацеплены `*_cgo.go`/tagged-тесты (ожидается: нет).
- Зачистка целиком — grep по семейству терминов (ожидаемый результат:
  0 совпадений вне docs/plans и истории ADR):
  `rg -n 'PolicyKind|policyKind|ParseRoomPolicy|ErrInvalidPolicy|invalid_policy|KindRoomFactory|AuthoritativeHost|authoritative-host|LOADGEN_POLICY|mapPolicy|PolicyMix|CreateRoomWithPolicy'`
- `rg -n 'STATE_POLICY_ALLOW_MASTER_MIGRATION'` — env, config README,
  plan/ADR.

## Post-review ремедиация (выполнено)

Ревью по Standards/Spec нашло остатки вне исходного скоупа — дочищено
по решению пользователя: `LOADGEN_POLICY_MIX` из
`scripts/loadtest-run.sh` + `deploy/loadgen/docker-compose.loadgen.yml`,
мёртвая метрика `AuthRoomsDied`/`auth_rooms_died` (loadgen metrics.go,
report.go), write-only `peer.host`/`peerSpec.host` (loadgen), stale
формулировки «authoritative-host (P29)» в client-protocol
(errors.md/enet.md), delivery README, usecase/room README+комменты,
opcodes.go, join-error-opcodes.mdc, дубль в domain/README; тест-файлы
переименованы в `no_migration_*`. Grep-контракт verification'а — 0
совпадений вне docs/plans + истории ADR.
