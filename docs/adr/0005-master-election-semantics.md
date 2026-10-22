# Семантика master election: skip-stale, validate-first, masterless-дедуп логов

Дивергенция `state.Peers`↔`r.peers` возможна (peer выходит из room с упавшим broadcast — запись остаётся в state до ближайшего тика `refreshPeerPings`). Старый `electMasterPeerID` падал на первом же stale-кандидате: любая ошибка `room.GetPeer` фейлила выборы целиком — уход master'а при stale-записи в state ронял весь `OnLeave`. Хуже, `assignMaster` сначала писал `IsMaster`-флаги в `roomState`, и только потом брал peer'а через `GetPeer`: при не-propagate'нувшемся отказе state на тиках `refreshPeerPings`/lazy RPC re-elect (там мутация `p.state` напрямую, без stage-commit) рекламировал master'а, которого у policy нет. Плюс `refreshPeerPings` логировал `Error` на каждом тике (~20/с), пока election продолжал падать.

## Decision

- **Skip-stale (D1).** `ErrPeerNotFound` от `room.GetPeer` в `electMasterPeerID` пропускает кандидата — выборы идут среди живых. Если все записи `state.Peers` stale — пустые выборы (`PeerIDInvalid`, `master = nil`, не ошибка). Прочие ошибки `GetPeer` по-прежнему фейлят выборы — невозможность прочитать живого peer'а хуже masterless-эпизода.
- **Validate-first в `assignMaster` (D2).** `GetPeer(masterID)` идёт до записи `IsMaster`-флагов: при ошибке state не мутируется и никогда не рекламирует master'а, которого нет у policy. Флаги сбрасываются остальным peers по-прежнему в том же цикле.
- **Retry — tick-based + lazy.** Переизбрание не форсируется: `refreshPeerPings` повторяет election на каждом patch-тике, пока `p.master == nil` и есть живые peers; RPC→master делает ленивое переизбрание по живому state и только потом отвечает `ErrNoMaster`.
- **Дедуп логов masterless-эпизода (D3).** Флаг `masterlessLogged` в policy: `Error` один раз при входе в эпизод (первый упавший re-elect), `Info` — при успешном назначении master любым путём (tick re-elect, lazy RPC re-elect, join). Эпизод, закончившийся без master'а (комната опустела), флаг не сбрасывает.

## Considered Options

- **Fail-whole election на stale** — отклонено: записи state отстают от `r.peers` на длину тика как штатная ситуация; падать из-за гарантированно мёртвого кандидата — самосаботаж failover'а.
- **Write-flags-then-validate** — отклонено: на путях прямой мутации `p.state` (`refreshPeerPings`, lazy re-elect) отказ `GetPeer` оставлял wire-visible `is_master=true` при `p.master == nil` — клиент видел мастера, которого сервер не знает.
- **Форсированный re-elect вне тика** — отклонено: patch-тик уже ходит каждые 50 ms; отдельный механизм не ускоряет восстановление заметно, а сложность добавляет.
- **Rate-limit лога по времени/счётчику тиков** — отклонено в пользу эпизодного флага: один `Error` на эпизод точнее отражает incident, чем «раз в N тиков».

## Consequences

- Уход master'а при stale-записях в state больше не фейлит `OnLeave` — выборы находят живого кандидата.
- `is_master` на wire никогда не указывает на peer'а, отсутствующего в policy; masterless-эпизод виден клиентам как «ни у кого нет флага» и закрывается ближайшей успешной election.
- Все-stale выборы — штатный «нет master'а», а не ошибка; комната остаётся без master'а до появления живого кандидата (join/восстановление записи).
- Логи перестают спамить `Error` 20 раз в секунду при затянувшемся сбое election — один `Error` на эпизод, `Info` при восстановлении.
- Цена skip-stale: кандидат, пропавший из room между elect и assign, всё ещё ловится `GetPeer` в `assignMaster` (validate-first) — окно гонки закрыто проверкой, а не обещанием свежести `peers`.
