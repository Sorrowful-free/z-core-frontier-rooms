# AGENTS.md

- `.cursor/rules/*.mdc` — always-on правила: зоны доверия (ai-companion), Go-стандарты, config, package-docs, join-error-opcodes, no-prerelease-compat.
- `GLOSSARY.md` — доменные термины.
- `docs/architecture.md`, `docs/client-protocol/`, `docs/wire-state-codec.md` — контракты и протокол.
- `docs/adr/` — принятые решения; `docs/plans/*.md` — планы работ, список файлов в плане = зафиксированный скоуп правок.
- Перед стартом плана, написанного до последних влитых P-коммитов, — сверить контракты (env, wire, ticket, HTTP API) с фактом кода; дрейф фиксировать правкой плана до диспатча.
- Lint: `~/go/bin/golangci-lint run ./...` (конфиг `.golangci.yml`, бинарь не на PATH); быстрый прогон тестов — `go test -race -shuffle=on -short -count=1 ./tests/...`; CI-эквивалент — без `-short` (подхватывает e2e `TestE2E*`, ~2 мин параллельно).
- ENet-файлы (`*_cgo.go`, тег `enet`) на хосте без `libenet-dev` не компилируются — локальная верификация через Docker: `./scripts/enet-verify.sh` (образ `zcore-rooms-dev` из `.devcontainer/Dockerfile`; build/vet/test под `-tags enet`). CI enet smoke-джоба — окончательная проверка.
- Правка любого файла под тегом `enet` (`*_cgo.go`, tagged-тесты) → обязательны `./scripts/enet-verify.sh build` **и** `vet` до отчёта/коммита: untagged `go build`/`vet`/lint их не видит, ошибка в tagged-коде иначе доезжает до CI. Gopls-предупреждения `No packages found` на этих файлах — ожидаемо (gopls без `-tags enet` в buildFlags), не ошибка.
- Новый `TestE2E_*` → своя job в `.github/workflows/test.yml` по паттерну `e2e-ws`/`e2e-enet`/`e2e-mixed` (отдельный статус, свой timeout): e2e не собираются в общую джобу.
- Сабагенты: в таске обязателен явный verification-блок (команды + ожидаемый результат); после отчёта агента с кодовыми правками parent прогоняет `go build ./...` до любых дальнейших шагов — агент мог умереть посередине с полу-edited деревом.
- Задачи «удалить X целиком» (формат, env, API): verification включает grep по семейству терминов, а не по механизму (пример P28: искать `v2|v3|v4|HostIDAny|ENET_LISTEN_PORT`, а не только `tokenVersionV2`); остатки перечисляются в отчёте.
