# Pre-release: обратная совместимость не поддерживается

До первого релиза в репозитории накопились compat-слои, обслуживающие форматы и контракты, которых нет ни в проде, ни у внешних клиентов: версионный диспатч ticket'ов (v2/v3/v4 с fallback-ветками verify), env-alias `ENET_LISTEN_PORT`, legacy-baseline `legacyGlobalReservation` в бенчмарках reservation, guard-тест старого wire layout'а и migration-ноты в документации. Каждый такой слой размывает единственную актуальную модель и тащит за собой упрощения, которые он сам же и требует (привилегия `HostIDAny` «admit на любом host'е» — дыра в strict binding P18).

## Decision

- Политика: pre-release compat не поддерживаем. Ломающие изменения формата / API / env делаются чисто — старое удаляется в той же задаче, без shim'ов и dual-format accept (зафиксировано также в `.cursor/rules/no-prerelease-compat.mdc`).
- Первый байт credential'а admission — `kind`, не `version`: `1` = join-ticket, `2` = reconnect-токен. Один layout на kind, `Verify` диспатчит по kind-байту; версионного диспатча и fallback-веток нет.
- Strict binding без дыр: `claims.HostID == 0` — `HostIDUnbound` («нет привязки») и существует только при `hostRegistry == nil` (WS-only deploy). На ENet-admit unbound-тикет отклоняется `ErrWrongHost` → `OpWrongHost` (`0x77`), слот reservation не тратится.
- Env-контракт единый: `ENET_LISTEN_PORTS` (CSV); alias `ENET_LISTEN_PORT` удалён, заданное значение молча игнорируется.
- Legacy-артефакты удалены вместе с кодом, который они покрывали: `legacyGlobalReservation` (benchmark baseline), guard-тест `decodePatch_legacyPasswordFlag`, migration-ноты из env-документации и комментариев.

## Considered Options

- **Оставить приём старых тикетов (v2)** — отклонено: старых тикетов в природе нет (релизов не было), а ветка verify и тест-хелпер `issueTicketV2` поддерживают мёртвый формат.
- **Оставить `HostIDAny`-привилегию для unbound на ENet** — отклонено: unbound отличается от любого валидного `host.id`, естественная проверка `claims.HostID != enetHostID` закрывает дыру без отдельной ветки; unbound нужен только WS-admit'у, где `enetHostID` не валиден и проверка не применяется.
- **Оставить `ENET_LISTEN_PORT` как deprecated-alias** — отклонено: два пути к одному параметру — это источник рассинхрона env'ов; один контракт `ENET_LISTEN_PORTS` покрывает и single-port деплой (`ENET_LISTEN_PORTS=7777`).

## Consequences

- Канонические layouts: join-ticket `kind(1)=1 | roomID | peerID | issuedAt | expiresAt | hostID | nickLen | nick | MAC`; reconnect-токен `kind(1)=2 | roomID | peerID | hostID | MAC` (51 байт). Значения перенумерованы `3/4 → 1/2` — все ранее выписанные credential'ы невалидны.
- `HostIDAny` переименован в `HostIDUnbound`; на ENet-admit unbound-тикет получает `0x77` вместо прохода — это задокументировано в client-protocol как строгость, а не совместимость.
- Новый compat-код (version-ветки, env-alias'ы, dual-format accept) не вводится без явного запроса — см. `.cursor/rules/no-prerelease-compat.mdc`.
- Исторические документы (`docs/adr/0001–0009`, `docs/plans/P*`) не правятся: они фиксируют решения на момент времени; актуальная модель — в пакетных README и `docs/client-protocol/`.
