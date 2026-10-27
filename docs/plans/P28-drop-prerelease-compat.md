# P28 — drop pre-release backward compatibility

## Контекст

До первого релиза в репозитории накопились костыли обратной совместимости.
Релизов не было — совместимость не нужна, она только захламляет код, тесты
и документацию. План зачищает всё найденное и упрощает модели, которые
compat-код тащил за собой.

Загриллено (итоговая модель):

- **Версионность тикетов убирается целиком.** Первый байт credential'а —
  не `version`, а `kind`: `1` = join-ticket, `2` = reconnect-token. Один
  layout на kind, никаких fallback-веток. Значения перенумерованы `3/4 → 1/2`.
- **Strict binding без дыр.** `claims.HostID == 0` = «нет привязки»
  (`HostIDUnbound`) и существует только при `hostRegistry == nil` (WS-only
  deploy). На ENet-admit unbound-тикет отклоняется `ErrWrongHost` — привилегия
  «admit на любом host'е» удалена. `HostIDAny` → `HostIDUnbound`.
- **`ENET_LISTEN_PORT` alias удалён** — единственный env-контракт это
  `ENET_LISTEN_PORTS` (CSV).
- **`legacyGlobalReservation`** (benchmark baseline) удалён; bench-файл
  упрощён до per-room прогонов.
- **Guard-тест** `decodePatch_legacyPasswordFlag` удалён — старому формату
  нечего ломать, regression-окна нет.
- **ADR-0010** фиксирует политику «pre-release compat не поддерживаем».
- Исторические документы (`docs/adr/0001–0009`, `docs/plans/P*`) — НЕ
  трогаем: они фиксируют решения на момент времени.

## Зона / override

Файлы prod-кода (`internal/**`, `cmd/**`) — красная зона; этот план с явным
списком путей = зафиксированный скоуп override по ai-companion. Правки делает
ИИ по списку ниже. Tests и docs — зелёная.

## Решения

### Ticket: version → kind

`internal/adapter/admission/admission.go`:

- Удаляются константы: `tokenVersionV2`, `tokenVersionV3`, `tokenVersionV4`,
  `fixedPayloadSizeV2`, `minTokenSizeV2`, `nickLenOffsetV2`, `nickOffsetV2`,
  `hostIDOffsetV3`, `nickLenOffsetV3`, `nickOffsetV3`, `hostIDOffsetV4`.
- Вводятся:

```go
tokenKindJoin      = 1
tokenKindReconnect = 2
fixedPayloadSizeJoin      = 1 + 8 + 8 + 8 + 8 + 2 + 1 // kind|roomID|peerID|issuedAt|expiresAt|hostID|nickLen
fixedPayloadSizeReconnect = 1 + 8 + 8 + 2             // kind|roomID|peerID|hostID
tokenSizeReconnect        = fixedPayloadSizeReconnect + macSize
minTokenSizeJoin          = fixedPayloadSizeJoin + macSize + 1 // + минимум 1 байт nick
hostIDOffset     = 33
nickLenOffset    = 35
nickOffset       = 36
hostIDOffsetReconnect = 17
```

- `Issue` пишет `buf[0] = tokenKindJoin` (layout без изменений).
- `IssueReconnect` пишет `buf[0] = tokenKindReconnect`.
- `verify`: диспатч по `token[0]` — `tokenKindJoin` → parse join,
  `tokenKindReconnect` → `verifyReconnect`, иное → `ErrInvalidToken`.
  Switch по версиям и v2-ветка удаляются; join-parse упрощается до
  фиксированных offset'ов без per-version переменных.
- Комментарии переписываются: «ticket v2/v3/v4» → «join-ticket /
  reconnect-token, kind byte».

### Strict binding: HostIDAny → HostIDUnbound

- `internal/domain/type_aliases.go`: `HostIDAny` переименована в
  `HostIDUnbound` (= 0); комментарий: «отсутствие привязки к host'у —
  ticket выписан без HostRegistry (WS-only); на ENet-admit отклоняется».
- `internal/usecase/room/admit_peer.go`: проверка становится

```go
if enetHostID.IsValid() && claims.HostID != enetHostID {
    ... return PeerSession{}, domain.ErrWrongHost
}
```

  (`claims.HostID != domain.HostIDAny` уходит — unbound `0` отличается от
  любого валидного hostID и отклоняется естественно). Комментарий Admit:
  `HostIDUnbound` = admit не по ENet (WS), проверки нет.
- `internal/delivery/ws/room_handler.go`: `domain.HostIDAny` →
  `domain.HostIDUnbound`, комментарий.
- `internal/usecase/room/issue_ticket.go`: комментарии (строки ~34, ~43–44):
  nil registry → `claims.HostID = HostIDUnbound`, тикет admit'ится только
  по WS.
- `internal/port/admission/admission.go`: doc-комментарий `Issue`:
  `HostIDUnbound` — без привязки; на ENet отвергается, живёт только для WS.
- `internal/domain/claims.go`: комментарии `Kind`/`HostID` — без версий.

### Config: снос ENET_LISTEN_PORT

`internal/adapter/config/load.go`:

- Константа `envEnetListenPort` удаляется.
- `loadEnetFromEnv`: ветка single-port удаляется, остаётся только CSV
  `ENET_LISTEN_PORTS`; комментарий функции без «backward-compat alias».
- Комментарий `loadReservationFromEnv` чистится от упоминания
  `RESERVATION_ORPHAN_ADMITTED_TTL` (env уже не читается — нота мёртвая).

`.env.example`: строка `# ENET_LISTEN_PORT=7777` удаляется.

### Reservation bench: без legacy-baseline

- `tests/adapter/reservation/legacy_global_reservation.go` — файл удаляется.
- `tests/adapter/reservation/reservation_bench_test.go` — параметр
  `legacy bool` и ветки `global_legacy` удаляются; остаются per-room
  прогоны (`benchmarkIssueJoinPath`, `benchmarkReserveParallelSpread`
  упрощаются до одной реализации).

## Файлы (скоуп)

### Prod (красная, override этим планом)

- `internal/adapter/admission/admission.go` — kind byte, снос v2, упрощение verify
- `internal/adapter/config/load.go` — снос `ENET_LISTEN_PORT`, чистка комментариев
- `internal/domain/type_aliases.go` — `HostIDAny` → `HostIDUnbound`
- `internal/domain/claims.go` — комментарии
- `internal/usecase/room/admit_peer.go` — strict check без Any-привилегии
- `internal/usecase/room/issue_ticket.go` — комментарии
- `internal/delivery/ws/room_handler.go` — `HostIDUnbound`

### Docs (зелёная)

- `internal/adapter/admission/README.md` — layouts переименованы в
  join/reconnect (kind `1`/`2`), секция v2 удаляется
- `internal/adapter/config/README.md` — строка `ENET_LISTEN_PORT` и нота
  про `RESERVATION_ORPHAN_ADMITTED_TTL` удаляются
- `internal/delivery/enet/README.md` — strict binding: unbound → `OpWrongHost`;
  сноска про `ENET_LISTEN_PORT` alias удаляется; «ticket v2» упоминания чистятся
- `internal/delivery/ws/README.md` — `(v3/v2/v4)` → join/reconnect;
  `HostIDAny` → `HostIDUnbound`
- `internal/usecase/room/README.md` — `HostIDAny` → `HostIDUnbound`
- `internal/domain/README.md` — `HostIDAny` → `HostIDUnbound`
- `internal/port/admission` — см. prod (комментарий, красная — в override)
- `README.md` (корень) — «Admission (ticket v2)» → «Admission (ticket)»
- `docs/client-protocol/realtime.md` — секция «Ticket v2» переписывается
  под join-ticket (kind `1`, layout с `host_id`); формулировки «v4» →
  «reconnect-token (kind `2`)»; layout kind=2
- `docs/client-protocol/enet.md` — строка про alias из env-таблицы;
  «ticket v3/v2» → join-ticket (kind `1`); «token v4» → kind `2`;
  проверка `ticket_bytes[0] == 3` → `== 1`; breaking-note P6d убирается
- `docs/client-protocol/http.md` — «ticket v3 / токен v4» → join-ticket /
  reconnect-token
- `docs/client-protocol/errors.md` — «reconnect_token v4», «version» →
  kind-терминология
- `docs/client-protocol/state-sync.md` — «(v4)» → reconnect-token
- `docs/wire-state-codec.md` — breaking-note P6d (про удалённый password
  flag) убирается — старого формата нет, предупреждать некого
- `GLOSSARY.md` — ReconnectToken: «ticket v4», «по версии/типу» →
  «kind byte»; HostID-термин дополняется «unbound → reject на ENet»
- `docs/adr/0010-drop-prerelease-compat.md` — НОВЫЙ: политика «pre-release
  compat не поддерживаем»; canonical kind `1`/`2`; единственный env-контракт;
  unbound не проходит ENet
- `.env.example` — снос строки alias

### Tests (зелёная)

- `tests/adapter/admission/admission_test.go` — удаляются
  `TestValidateTicketV2WithoutHostID` + helper `issueTicketV2`;
  `HostIDAny` → `HostIDUnbound`; проверки `token[0]` → kind;
  комментарии «v4» → reconnect
- `tests/adapter/config/load_test.go` — `ENET_LISTEN_PORT` удаляется из
  всех env-списков/кейсов; `TestLoadFromEnv_EnetOverrides` переводится на
  `ENET_LISTEN_PORTS`; `TestLoadFromEnv_EnetListenPortWinsOverPorts`
  удаляется; invalid-кейсы на single-port удаляются
- `tests/integration/enet_shard_test.go` — блок «Backward compat» (~199–214)
  переписывается в негативный: `Issue(..., HostIDUnbound)` в обход usecase →
  `admitRaw` на ENet → `0x77 OpWrongHost` + disconnect, слот не тратится
- `tests/usecase/room/admit_peer_test.go` — `HostIDAny` → `HostIDUnbound`;
  + кейс: `enetHostID` валиден + `claims.HostID = HostIDUnbound` →
  `ErrWrongHost` (unit-фиксация строгости)
- `tests/usecase/room/issue_ticket_test.go` — `HostIDAny` → `HostIDUnbound`
- `tests/adapter/reservation/legacy_global_reservation.go` — удалить файл
- `tests/adapter/reservation/reservation_bench_test.go` — упростить до
  per-room (без `global_legacy`)
- `tests/adapter/realtime/codec/room_state_codec_test.go` — удалить
  `TestRoomStateCodec_decodePatch_legacyPasswordFlag`
- `tests/README.md` — описания bench/admission/config-прогонов без
  legacy/v2/alias-упоминаний

### Не трогаем

- `docs/adr/0001–0009`, `docs/plans/P1–P27` — история.
- `internal/delivery/enet/session_cgo.go` и прочий tagged-код — нет
  compat-следа.
- `Issue`/`IssueReconnect` сигнатуры портов — hostID-параметр остаётся
  (unbound передаётся как `HostIDUnbound`).

## Verification

1. `go build ./...`
2. `~/go/bin/golangci-lint run ./...`
3. `go test -race -shuffle=on -short -count=1 ./tests/...`
4. `./scripts/enet-verify.sh build` и `vet` — правится tagged-тест
   `enet_shard_test.go`; полный `test` под тегом в Docker (enet e2e
   включая новый негативный сценарий) — по возможности, иначе CI enet
   smoke-джоба.
5. Полный CI-эквивалент `go test -race -shuffle=on -count=1 ./tests/...`
   перед пушем (e2e ~2 мин).

## Коммиты

- Коммит 1: этот план-док.
- Коммит 2: реализация P28 целиком (prod + tests + docs + ADR-0010) —
  раздельными остаются только план-доки. Тексты коммитов — согласовать
  перед `git commit`.
