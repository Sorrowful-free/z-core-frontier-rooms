# AGENTS.md

- `.cursor/rules/*.mdc` — always-on правила: зоны доверия (ai-companion), Go-стандарты, config, package-docs, join-error-opcodes.
- `GLOSSARY.md` — доменные термины.
- `docs/architecture.md`, `docs/client-protocol/`, `docs/wire-state-codec.md` — контракты и протокол.
- `docs/adr/` — принятые решения; `docs/plans/*.md` — планы работ, список файлов в плане = зафиксированный скоуп правок.
- Lint: `~/go/bin/golangci-lint run ./...` (конфиг `.golangci.yml`, бинарь не на PATH); CI-эквивалент тестов — `go test -race -shuffle=on -count=1 ./tests/...`.
- ENet-файлы (`*_cgo.go`, тег `enet`) на хосте без `libenet-dev` не компилируются — локальная верификация через Docker: `./scripts/enet-verify.sh` (образ `zcore-rooms-dev` из `.devcontainer/Dockerfile`; build/vet/test под `-tags enet`). CI enet smoke-джоба — окончательная проверка.
- Сабагенты: в таске обязателен явный verification-блок (команды + ожидаемый результат); после отчёта агента с кодовыми правками parent прогоняет `go build ./...` до любых дальнейших шагов — агент мог умереть посередине с полу-edited деревом.
