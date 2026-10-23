# P18: шардирование ENet-хостов (несколько UDP-listener'ов)

## Контекст

HostLoop (P15) убрал глобальный apiMu → теперь каждый host независим.
Room — транспорт-агностик: mixed e2e доказал, что peer'ы на разных
транспортах живут в одной комнате; значит peer'ы разных ENet-хостов тоже
могут. Шардирование = N listener'ов (N host'ов, N UDP-портов, N HostLoop'ов).

Потолок одного host'а: `Service` гоняет ВСЕ пакеты всех peer'ов через один
loop → pps/latency bound; PeerLimit ≤ 4095 на host.

## Решения (grill-with-docs)

- **Q1 гранулярность — peer**: peer'ы одной комнаты размазаны по хостам
  (шард = peer). Room-loop уже общий и транспорт-агностичен — host даёт
  масштаб по пакетному I/O, а не по state. Клиенту неважно, на каком
  host'е живут соседи по комнате.
- **Q2 discovery — issue-ticket**: ответ `POST /rooms/{id}/tickets`
  возвращает порт назначенного host'а (например `enetPort` в JSON).
  Балансировка серверная: least-loaded по живым peer'ам host'а
  (round-robin как fallback при равенстве).
- **Q3 привязка — строгая**: тикет несёт hostID в claims; admit на чужом
  host'е → join-reject кадр (новый код в band 0x70-0x7F reservation или
  существующий подходящий). Claims меняются → ADR/док admission.
- **Q4 конфиг — список**: `ENET_LISTEN_PORTS=7777,7778,7779` (CSV,
  1..N портов, дубликаты → ошибка валидации). `ENET_LISTEN_PORT`
  остаётся alias для single-port (backward compat: если задан PORT —
  он побеждает, иначе PORTS).
- **PeerLimit**: per-host как сейчас (лимит структуры libenet);
  ёмкость кластера = N × PeerLimit, room capacity не привязана к host'у
  (room — транспорт-агностик).
- **Shutdown**: `RoomsHandler` становится mux'ом над N host-loop'ами;
  `ListenHandle.Stop()` fan-out по всем, `Done()` закрывается когда
  все вышли (errgroup-семантика).

## Скоуп (по решениям)

- `adapter/admission`: hostID в ticket claims + валидация
- `adapter/config`: `ENET_LISTEN_PORTS` CSV-парсер + валидация
  (диапазон, дубликаты), alias `ENET_LISTEN_PORT`
- `delivery/enet`: один handler → N HostLoop'ов (по одному на порт);
  admit проверяет claims.hostID против своего host'а; least-loaded
  assignment живёт в usecase (handler только проверяет)
- `usecase/room/IssueTicket`: выбор host'а (least-loaded) + hostID в
  claims + порт в ответ
- `delivery/http`: `enetPort` в ticket response DTO
- `tests/testutil/env`: `WithEnet(ports ...uint16)` несколько host'ов
- e2e: сценарий 2 host'а — клиенты admit на назначенные порты, комната
  собирает peer'ов с разных host'ов, reject на чужом порту
- Docs: `docs/client-protocol/enet.md`, `http.md`, admission README, ADR

## Замечание по реализации

Strict-binding требует чтобы Claims нёс hostID — claims подписываются/
шифруются admission-адаптером: новое поле обратно-совместимо (старые
тикеты без hostID → admit на любом host'е? Решить при реализации:
проще — отсутствие hostID = любой host, backward compat бесплатно).

## Верификация

- Docker tagged прогон; latency-замер до/после на 2+ host'ах
