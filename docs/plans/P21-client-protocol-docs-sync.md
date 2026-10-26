# P21 — client-protocol docs sync под P20/P24

## Контекст

Повторный аудит P20: код соответствует спеке, но часть client-protocol
документации описывает до-P20 семантику или не фиксирует принятые
нюансы. Доки — по факту кода (ai-companion), ничего «про будущее»,
кроме явно помеченного блока (c), который дописывается после P24.

## Что фиксируем (загриллено: a+b сейчас, c после P24)

### a) `state-sync.md` — секция «Replace (reconnect)»

Переписать под P20-модель (сейчас там «нужен новый issue ticket» —
противоречит realtime.md):

- disconnect → peer удаляется из комнаты, остальным 0x80
  `OpPeerDisconnected`; grace живёт на admitted-слоте
  (`lastSeen + RECONNECT_TTL`);
- resume — `reconnect_token` (v4), тот же peerID, nick из слота;
- **мастерство не возвращается**: вернувшийся — обычный peer,
  election уже прошёл при disconnect;
- resume при живом старом conn — Replace (newest-wins), старый conn
  получает разрыв.

### b) `errors.md` / `realtime.md` — нюансы

- Resume по уже удалённой комнате → `OpRoomNotFound` 0x43
  (не `OpPeerNotFound` — осознанное отклонение от буквы T10 плана P20,
  код точнее).
- «0x81 без предшествующего 0x80» — нормальная ситуация при Replace
  живого peer'а (двойной resume / перехват соединения): клиент обязан
  обрабатывать reconnected идемпотентно.
- Resume-reject карта (актуальная): `OpInvalidToken`/`OpExpiredToken` —
  токен; `OpReservationNotReserved` 0x73 — слота нет/не admitted;
  `OpRoomNotFound` 0x43; `OpWrongHost` 0x77 — hostID тикета v4 ≠ host.

### c) После P24 — enforce дедлайна grace на resume

- В `realtime.md` (+ при желании state-sync): resume по просроченной
  grace (peer не в комнате и `lastSeen + RECONNECT_TTL` прошёл) →
  reject `OpReservationExpired` 0x74; дедлайн проверяется до refresh'а
  lastSeen — отклонённые попытки не продлевают grace. При живом peer'е
  дедлайн не применяется (Replace newest-wins).

## Скоуп правок (фиксированный)

- `docs/client-protocol/state-sync.md` — секция Replace.
- `docs/client-protocol/errors.md` — resume-reject карта + 0x43/0x74.
- `docs/client-protocol/realtime.md` — нюанс «0x81 без 0x80», (c).
- `docs/llms.txt` — только если индекс относится к затронутым якорям.

## Верификация

- Прочитать изменённые секции целиком: нет противоречий с
  `docs/client-protocol/realtime.md` (P20-блок уже влит), wire-layout
  не затронут.
- Кросс-ссылки: opcodes 0x80/0x81/0x74/0x43/0x77 совпадают с
  `internal/domain/opcodes.go` и `errors.md`.
