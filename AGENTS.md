# AGENTS.md

- `.cursor/rules/*.mdc` — always-on правила: зоны доверия (ai-companion), Go-стандарты, config, package-docs, join-error-opcodes.
- `GLOSSARY.md` — доменные термины.
- `docs/architecture.md`, `docs/client-protocol/`, `docs/wire-state-codec.md` — контракты и протокол.
- `docs/adr/` — принятые решения; `docs/plans/*.md` — планы работ, список файлов в плане = зафиксированный скоуп правок.
- Lint: `~/go/bin/golangci-lint run ./...` (конфиг `.golangci.yml`, бинарь не на PATH); CI-эквивалент тестов — `go test -race -shuffle=on -count=1 ./tests/...`.
- ENet-файлы (`*_cgo.go`, тег `enet`) локально не компилируются без `libenet-dev` — верификация через CI enet smoke-джобу, правки только внимательным чтением.
