# Reconnect: delete-peer grace на admitted-слоте reservation (P20)

До P20 любой разрыв транспорта был фатален для игровой идентичности: `LeaveRoom` изымал peer'а, `Revoke` освобождал слот, peerID умирал навсегда — вернуться можно было только новым ticket'ом под новым peerID, потеряв entity-ownership и мастерство. Для флаки-соединений короткий разрыв убивал сессию. Нужна была grace-модель: короткое окно, в котором клиент воскрешается под тем же peerID.

Ключевой вопрос дизайна: **где живёт состояние «peer в grace»**. Кандидат-фаворит keep-peer (peer остаётся в `r.peers` со статусом disconnected) создавал дублирующий источник истины: факт разрыва кодировался бы одновременно в мёртвом conn (`IsAlive=false`), остановленном peer-объекте и отдельном deadline-хранилище. Каждому потребителю пришлось бы объяснять состояние «объект есть, но он мёртв»: `Send` — фильтровать stopped peer'ов (иначе «peer stopped» error на каждом broadcast), `electMasterPeerID` — проверять `IsAlive` (иначе disconnected peer мог быть переизбран мастером), остановка goroutine'у — иначе утечка outbound-горутины на каждый разрыв.

## Decision

- **Disconnect = полный leave минус revoke.** Peer и conn удаляются сразу обычным `room.Leave`-путём: state-запись чистится, election мгновенный (мастерство не возвращается — вернувшийся мастер становится обычным peer'ом), remove-патч уходит остальным. Peer exists ⟺ connected.
- **Grace живёт на admitted-слоте reservation.** Слот — единственная сущность, переживающая peer'а; он же удерживает capacity (`Reserve` считает занятые слоты → новых ticket'ов нет). `reservation.MarkDisconnected(roomID, peerID)` обновляет `admittedAt` (переиспользован как lastSeen); `deadline = lastSeen + RECONNECT_TTL` — серверное состояние, токен expiry не несёт. Expiry — существующий `SweepOrphanAdmittedUseCase`: слот без peer'а и истёкший deadline → `Revoke` (leave-путь не нужен, peer уже удалён). `RECONNECT_TTL` заменяет `OrphanAdmittedTTL` — при едином timestamp их роли сливаются.
- **Resume — отдельный reconnect-поток за единым admit-фасадом.** Оба data-plane handler'а отдают сырой token одному входу: `Validate` один раз → ветвление по `Claims.Kind`. Resume: `reservation.Reconnect(roomID, peerID)` — атомарная проверка `Admitted` + refresh `admittedAt` + возврат слота (закрывает гонку sweep-revoke ↔ resume) → `room.Join`, либо `room.Replace` если peer ещё жив (reconnect всегда побеждает: новейшее соединение вытесняет старое — покрывает UDP-lingering и быстрый реконнект). `reservation.Admit` не вызывается — слот уже admitted; слот не ревокается ни при каком фейле.
- **Reconnect-токен v4** — `ver | roomID | peerID | hostID | MAC` (51 байт), без nickName (nick берётся из `peerSlot.nickName` — слот единственный выживший источник) и timestamps; `Claims.Kind` отличает от join-тикета; ENet strict binding сохранён. Cross-transport resume разрешён.
- **Идентичность teardown'а — по peer-объекту.** Старый conn, проснувшийся после `Replace`, не должен выкинуть нового peer'а: lifecycle-опы удаляют из `r.peers` только если `r.peers[id] == переданный peer`, иначе `ErrPeerStale`. `PeerSession{RoomID, PeerID, Peer}` возвращается из Join/Reconnect и несётся handler'ом до teardown.
- **Zombie получают то же grace.** `EvictIdlePeersUseCase` направляет `!IsAlive` в тот же disconnect-путь — полуоткрытый TCP (целевой сценарий флаки-клиентов) не отличается от чистого разрыва.
- **Opcodes — отдельный класс room notifications 0x80+** (`OpPeerDisconnected`, `OpPeerReconnected`, payload `peerID u64`): рассылает use case через `room.Send` после leave/join — FIFO outbound гарантирует порядок «state-патч → opcode». Policy/lifecycle-интерфейсы не расширяются.
- **Мастер переизбирается при disconnect** (бесплатно — обычный `OnLeave`), не возвращается при resume.

## Considered Options

- **Keep-peer: статус + deadline на peer'е или в room-map** — отклонено: дублирует источник истины о разрыве (conn, stopped-объект, deadline), требует Send-фильтрации, IsAlive-правок election, аккуратной остановки goroutine'у; единственная выгода — peer остаётся видимым в `state.Peers` во время grace («серый аватар»), что не требовалось.
- **Grace = TTL reconnect-токена от issue** — отклонено: игрок, проживший дольше TTL, не смог бы реконнектиться (токен мёртв в момент обрыва); окно сокращалось бы с длиной сессии. Grace считается от момента разрыва, серверной стороной.
- **Resume через существующий `Replace` как единственный путь** — отклонено как недостаточный: под deleted peer'ом `Replace` неприменим (нужен `Join`); `Replace` остаётся для swap по живому peer'у.
- **Policy-хуки `OnPeerDisconnected/OnPeerReconnected` + lifecycle-опы mark/resume** — отклонено: в delete-peer модели disconnect — обычный leave, resume — обычный join; policy видит стандартные события, opcodes — оркестрационная обязанность use case.
- **Отдельное поле `disconnectedAt` в `peerSlot`** — отклонено в пользу переиспользования `admittedAt` как lastSeen: каждый admitted-слот без peer'а — это grace, отдельная метка не добавляет различий, зато дублирует временные ряды.

## Consequences

- Клиент видит peer'а **ушедшим** во время grace (remove-патч + `OpPeerDisconnected` = «может вернуться»), а не помеченным в списке. Игра, которой нужен «серый аватар», не сможет его построить из state.
- Все disconnect-пути (WS waiter, ENet EventDisconnect/bad-frame, idle sweep) ведут в `DisconnectPeerUseCase`; `LeaveRoomUseCase` остаётся для rollback'ов и явных leave — слот больше не освобождается мгновенно при разрыве: capacity удерживается до `RECONNECT_TTL` или resume.
- `runLeaveLocked` получает identity-check — попутно чинится существующая гонка `LeaveRoom(peerID)` после `Replace`; stale teardown отвечает `ErrPeerStale` (ожидаемая гонка → Debug-лог).
- `OrphanAdmittedTTL` перестаёт читаться: единый TTL слота — `RECONNECT_TTL` в `ReservationConfig` от `admittedAt`(lastSeen); заданный env молча ни на что не влияет (миграция задокументирована).
- Взломанный/утёкший reconnect-токен позволяет вытеснить живого peer'а жертвы в пределах grace — тот же класс угрозы, что и украденный join-тикет до ADR-0004, теперь ограниченный окном `RECONNECT_TTL`.
- In-flight RPC/кадры к отключившемуся peer'у получают `ErrPeerNotFound` — как при любом leave; захват entities — логика мастер-клиента по `OpPeerDisconnected`.
