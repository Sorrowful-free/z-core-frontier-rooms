# P6e — Тесты, docs, ADR-0004 (зелёная зона, после P6a–P6d)

> **Статус:** done кроме: консолидация рукописных стабов (отложена — скриптовые harness'ы не сводятся к gomock без потери читаемости) и fuzz codec (отдельной задачей).

## Контекст

Завершающий чанк: всё в зелёной зоне (`tests/**`, docs), ИИ делает сам. Запускать **после** P6a–P6d — регрессионные тесты фиксируют новую семантику, ADR пишется по решённому, а не предполагаемому.

## Зависит от

P6a, P6b, P6c, P6d.

## Скоуп

- `tests/**` (включая `tests/mocks/generate.go`, `tests/testutil/**`)
- `docs/adr/0004-ticket-lifecycle-semantics.md` (новый)
- Docs-файлы из списка ниже; `README.md`, `GLOSSARY.md`
- `docs/plans/*.md` — статусы

## Шаги

1. `tests/adapter/reservation/reservation_bench_test.go`: `BenchmarkReservation_Reserve` — `Revoke` в цикле (сейчас `b.Fatalf` на 17-й итерации: capacity 16, unique peerID, нет revoke).
2. `tests/mocks/generate.go`: добавить `RoomPolicy`, `RoomPolicyFactory`, `RoomFactory`, `WsConnectionFactory`, `EnetConnectionFactory`; `go generate ./tests/mocks/...`; консолидировать рукописные стабы (8 Peer, 5 policy, логгеры, `noopConnection`) — заменить моками где равнозначно.
3. Пробелы покрытия по приоритету: malformed-payload пути политики (`ErrInRoomInvalidPayload`, `ErrNoMaster`, `ErrInvalidRpcTarget`), `GetCapacity` clamp, `TransportConfig`/`ServerConfig` `Validate()`, WS token-too-large путь. Fuzz по codec — если успеется в этом чанке, иначе отдельной задачей.
4. `legacy_global_reservation.go`: комментарий со ссылкой на ADR/коммит, который он зеркалит.
5. `docs/adr/0004-ticket-lifecycle-semantics.md`: D3–D5 — one-time-use admit, leave-always-removes, stage-commit leave-ветки, nick-dedup, почему `ADMISSION_PASSWORD` удалён. Формат — как ADR-0001/0003.
6. `GLOSSARY.md`: «orphan admitted», «ghost input» — если термины укоренились в коде P6a/P6b.
7. Docs-фиксы (подтверждены аудитом):
   - формула тикета `67+len(nick)` → `66+len(nick)`: `docs/client-protocol/enet.md`, `realtime.md`, `internal/adapter/admission/README.md`
   - `http.md`: `invalid_room_id` для невалидного room id (POST tickets + DELETE)
   - `policy/state/README.md`: master-only только entities; input/RPC — от всех peers
   - корневой `README.md` TODO: убрать «WS e2e» (сделано), оставить health-check
   - `docs/adr/0002`: следствие «fasthttp/websocket только в tests/» устарело — он в production
   - `delivery/errors`/`errors.md`: `IsJoinRejectOpCode` vs реальные диапазоны `0x40–0x5F`+`0x70–0x7F`; маршрут `POST /rooms/:id/tickets`
   - `docs/architecture.md`: полный список портов
   - `internal/domain/README.md:29`: направление зависимости `InRoomErrorOpCode`
   - `tests/README.md:151`: «dead» → glossary-термин
   - `config/README.md` + `transport/README.md`: `<=0 disables` → только `0` (Validate отвергает отрицательные)
   - `delivery/enet/README.md:18`: admit-диапазон не включает `0x60–0x6F`
8. `docs/plans/P1–P5`: пометить завершёнными (всё подтверждено аудитом); в `P6-*` проставить статусы по мере мержа чанков.

## Проверки

```bash
go generate ./tests/mocks/... && go build ./... && go vet ./...
~/go/bin/golangci-lint run ./...
go test -race -shuffle=on -count=1 ./tests/...
go test -bench=BenchmarkReservation_Reserve -run=^$ ./tests/adapter/reservation/
```
