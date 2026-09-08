# Образ для devcontainer: Go 1.25 + зависимости ENet (cgo).
# Пакеты ставятся на этапе сборки образа (под root), чтобы lifecycle-скрипты
# Zed/VS Code оставались простыми и не зависели от sudo.
FROM mcr.microsoft.com/devcontainers/go:1.25-bookworm

# Зависимости ENet — как в CI (.github/workflows/test.yml, job enet),
# чтобы работал `CGO_ENABLED=1 go build -tags enet ./cmd/rooms`.
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends gcc pkg-config libenet-dev \
    && rm -rf /var/lib/apt/lists/*
USER vscode
