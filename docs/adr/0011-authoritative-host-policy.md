# Room policy: master-migration — глобальный флаг вместо per-room kind

> История: изначально (P29) решение фиксировало per-room поле `policy` в
> `POST /rooms` (`domain.RoomPolicyKind`: `state` / `authoritative-host`) и
> отдельный тип `AuthoritativeHostPolicy` поверх `StateRoomPolicy`. P33
> разворачивает это: выбор режима — прерогатива деплоя, а не создателя
> комнаты; весь per-room kind-механизм удалён. Текст ниже описывает
> актуальное решение.

## Контекст

До P29 каждая комната получала `StateRoomPolicy` с автоматической
master-миграцией: уход master'а вёл к `electAndAssign` среди оставшихся.
Для сценария «авторитарный хост» (сервер матча живёт в клиенте-хосте) это
неприменимо: ушёл хост — матч окончен, перевыборы бессмысленны.

P29 ввёл per-room kind (`policy` в `POST /rooms`, `RoomPolicyKind`,
`KindRoomFactory`, две policy-фабрики, `policyKind`-параметр сквозь
port/usecase). Практика показала: смешанный деплой (мигрирующие и
authoritative-host комнаты в одном процессе) не нужен — режим определяется
оператором сервиса, а цена kind-цепочки (dispatch-фабрика, параметр через
четыре уровня, DTO-валидация) оказалась избыточной.

## Decision

- **Режим — глобальный флаг деплоя**: env
  `STATE_POLICY_ALLOW_MASTER_MIGRATION` (дефолт `true` — миграция
  разрешена, историческое поведение). ModuleConfig
  `state.StatePolicyConfig{AllowMasterMigration bool}` +
  `Validate()` в `adapter/realtime/policy/state`; поле
  `Config.StatePolicy`, парсинг в `adapter/config/loadStatePolicyFromEnv`.
- **`allowMasterMigration` — поле `StateRoomPolicy`**: при
  `OnLeave` уходящего master'а и `allowMasterMigration == false` policy
  возвращает sentinel `domain.ErrRoomShouldClose` (проверка до входа в
  master-машину — peer ещё в state); уход non-master — обычный путь.
  Фабрика `StateRoomPolicyFactory` принимает `StatePolicyConfig` в
  конструкторе и вливает флаг **полем** (прецедент `p.master.metrics` —
  сигнатура `NewStateRoomPolicy` стабильна для тестов).
- **Teardown без изменений**: `ErrRoomShouldClose` → usecase `closeRoom`
  (`OpRoomClosed 0x82` оставшимся → `DeleteRoom` → `UnregisterRoom` →
  drain-aware `peer.Stop`); `RoomPolicyFactory.CreateRoomPolicy()` и
  `masterMachine` не трогаются.
- **Per-room kind удалён целиком** (no-prerelease-compat): поле `policy`
  в `POST /rooms`, `RoomPolicyKind`, `ParseRoomPolicyKind`,
  `ErrInvalidPolicy`, `codeInvalidPolicy`, `AuthoritativeHostPolicy` +
  `AuthoritativeHostPolicyFactory`, `KindRoomFactory`, параметр
  `policyKind` в `port/realtime.RoomFactory`,
  `port/registry.RoomRegistry`, `adapter/registry`, `CreateUseCase.Create`,
  `LOADGEN_POLICY_MIX` и `body["policy"]` в loadgen.

## Considered Options

- **Оставить per-room поле поверх env-дефолта** — отклонено: смешанный
  режим не нужен, а оба пути конфигурации плодят рассинхрон («клиент
  попросил state при no-migration деплое»).
- **`NewStateRoomPolicy(..., allowMasterMigration bool)`** — отклонено в
  пользу полевого влейва: metrics уже идёт полем именно ради стабильной
  сигнатуры для тестов; флаг — тот же случай (внешние тесты строят
  no-migration policy через фабрику с `StatePolicyConfig{false}`).
- **Флаг внутри masterMachine** — отклонено: `ErrRoomShouldClose` должен
  уйти наружу до staged-мутаций state; проверка на входе `OnLeave`
  сохраняет контракт «ошибка = нет побочек».
- **Новый ADR вместо правки на месте** — отклонено: 0011 переписан под
  актуальное решение, история разворота зафиксирована шапкой.

## Consequences

- Старые клиенты, шлющие `policy` в body create, получают молчаливый игнор
  (неизвестное JSON-поле) — поведение комнаты диктует сервер.
- В no-migration деплое **каждая** комната умирает с уходом master'а;
  `0x82` остаётся обязательным к обработке в Godot-клиенте.
- Тесты no-migration режима (ex-`authoritative_host_policy_test`,
  integration) строятся через `NewStateRoomPolicyFactory` с
  `StatePolicyConfig{AllowMasterMigration: false}` /
  `testutil/env.WithStatePolicy(...)` — прямого доступа к полю у внешних
  пакетов нет.
- `room.Leave` по-прежнему логирует `ErrRoomShouldClose` как
  `policy on leave failed` — разовая строка на смерть комнаты.
