# P11 — e2e match: live room, host migration

Зелёная зона: `tests/integration/` + `tests/testutil/env/` (env опция).
Production не трогаем. Клиенты — Go-горутины в тесте (по запросу —
«плохой код» допустим, цель — наблюдать wire-поток).

## Дизайн (зафиксирован гриллингом)

- **Сценарий ~60s реального времени**, `testing.Short()` → skip
- **4 клиента** по WS: host + 3 slave; каждый — горутина с read-pump
- **Клиенты state-driven**: slave непрерывно шлёт `PatchInput` (свой input,
  ~10 Гц); клиент, увидев `is_master` на себе в patch/full-state,
  начинает рассылать `PatchEntities` (позиции ~10 Гц); флаг снят — стоп
- **Логи**: `t.Logf` с префиксом ника (`[host] ...`) — единая
  хронологическая лента, видна при `-v` и при падении

## Механика отвала хоста

- `env` опция `WithIdleSweep(interval)` — поднимает настоящий
  `EvictIdlePeersUseCase` + `StartIdlePeerSweep` (прод-пайплайн)
- `WithTransport`: `PeerIdleTimeout=3s` (wsPingInterval=2s — меньше нельзя:
  здоровые клиенты живут за счёт pong → lastSeen), `PeerIdleSweepInterval=1s`
- Отвал хоста = его горутина перестаёт читать (conn открыт на TCP, pong не
  генерируется) → lastSeen стареет → sweep эвиктит за ≤ ~4s

## Хронология сценария

1. host create-room + join; 3 slave join по тикетам → 4 peers
2. ~10s: master=host шлёт позиции; slaves шлют input; slaves декодируют
   entities и видят позиции, input-ы видны всем в state
3. host замолкает (стоп read-pump, conn жива) → sweep eviction →
   миграция: `is_master` уезжает к одному из slaves → он начинает слать
   позиции сам (state-driven доказательство миграции)
4. ~15-20s жизни под новым мастером
5. новый мастер делает clean leave (close conn) → миграция №3
6. оставшиеся ≥2 клиента продолжают стрим до конца минуты

## Ассерты

- slaves наблюдали entities-позиции от host И от мастера-2 (магия
  переезда видна на wire)
- input каждого slave попадал в room state (виден остальным)
- обе миграции зафиксированы сменой `is_master` в принятых кадрах
- финал: ≥2 живых клиента, patches текут, room peers count сходится
  с registry

## Файлы

- `tests/testutil/env/env.go` — `WithIdleSweep(interval)` option: wiring
  EvictIdlePeersUseCase + StartIdlePeerSweep внутри New (cleanup в t.Cleanup)
- `tests/integration/match_e2e_test.go` — сценарий + wsClient helper
  (read-pump, send input/entities через существующие codec encoder'ы
  `codec.NewEntitiesStateCodec()/InputStateCodec()` — они же серверные)
- `tests/README.md` — строка про e2e match + как читать логи клиентов
