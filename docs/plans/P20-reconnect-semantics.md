# P20 — Reconnect semantics

## Контекст

Сейчас disconnect = `LeaveRoom`: peer изымается из комнаты, слот reservation
отзывается, peerID умирает навсегда. Вернуться можно только как **новый**
peer: новый тикет, новый peerID — весь entity-ownership, мастерство и
идентичность потеряны. Для сетевой игры с флаки-соединениями это делает
короткий разрыв фатальным для сессии игрока.

## Что уже есть (строительные блоки)

- `Room.Replace(peer)` — leave-then-join под тем же peerID (семантический
  разрыв ~один lifecycle-виток, зафиксировано в P13).
- `deliverFullStateToPeer` — point-to-point full state на join: механизм
  resync'а уже существует.
- Reservation Admit-слот: связка ticket→peerID, TTL через
  `OrphanAdmittedTTL` + orphan sweep.
- Ticket v3 уже несёт peerID — «resume ticket» это тикет на существующий
  peerID, не требует новых полей.
- Idle eviction (`EvictIdlePeersUseCase`) — существующий механизм вывода
  мёртвых peer'ов.

## Цель

Короткий разрыв соединения не убивает игровую идентичность: клиент
переподключается и продолжает как тот же peer (свои entities, имя,
потенциально мастерство).

## Решения (зафиксированы гриллингом)

- **Идентичность**: resume под тем же peerID. `issue-ticket` (и create-room)
  ответ несёт `reconnect_token` — отдельный HMAC-подписанный токен (родня
  ticket v3; внутри roomID + peerID + expiry; отличим от join-тикета
  по версии/типу). Одноразовый входной тикет и reconnect-credential —
  разные сущности с разными TTL.
- **Grace-модель**: disconnect не вызывает LeaveRoom немедленно — peer
  остаётся в комнате со статусом «disconnected», держит слот
  capacity/reservation. Grace = TTL reconnect-токена (одна константа —
  токен истёк ⇒ окончательный leave + слот освобождается).
- **Resume-путь**: клиент подаёт `reconnect_token` в тот же admit-канал
  (WS query / ENet первый пакет); `admission.Validate` распознаёт тип.
  PeerID из токена → `Room.Replace` подменяет connection →
  `deliverFullStateToPeer` resync → peer живой.
- **Мастер**: элекшн немедленно при disconnect (комната не простаивает);
  вернувшийся мастер — обычный peer, мастерство назад не возвращается.
- **Wire**: два новых in-room opcode — `OpPeerDisconnected`,
  `OpPeerReconnected` (broadcast остальным). Захват entities
  отвалившегося peer'а — логика мастер-клиента, не фреймворка.
- **Expiry disconnected**: по TTL токена — обычный leave (тот же путь,
  что явный leave: master machine, state cleanup, слот).

## Открытые точки (уточнить при реализации)

- Точный wire-layout reconnect-токена (версия/тип + поля).
- Peer-статус: флаг в peer/state vs отдельный реестр disconnected.
- Где тикает TTL-истечение (room loop vs sweep).
- Повторный disconnect до истечения — продление grace?

## Скоуп правок (черновик)

- `domain`: reconnect-claims/тип токена, новые opcode константы.
- `adapter/admission`: issue/validate reconnect-токенов.
- `usecase/room`: reconnect-pуть (resume через Replace), disconnect-mark
  вместо немедленного leave на transport-death.
- `realtime` + `policy/state`: статус disconnected, broadcast новых
  опкодов, TTL-истечение → leave.
- `delivery/*`: admit-пути принимают оба типа токенов.
- HTTP DTO: `reconnect_token` в issue-ticket/create-room ответах.
- client-protocol docs, ADR.

## Верификация

- инвариант-харнес (P10) + property-тесты на disconnect→resume
- e2e: разрыв → reconnect → full state resync → продолжение матча;
  expiry grace → настоящий leave
