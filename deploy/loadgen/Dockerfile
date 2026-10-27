# loadgen-образ (P27 inc2): dev-база (Go 1.25 + libenet, нужен для inc3
# `-tags enet`) + iproute2 для tc netem на egress. Сервер в образе нет —
# он снаружи (host/другой контейнер), адреса через LOADGEN_*_ADDR.
#
# Build ТОЛЬКО host-network (bridge DNS на машине мёртв, см. global rules):
#   docker build --network=host -t zcore-loadgen -f deploy/loadgen/Dockerfile .
# (docker compose build не умеет host-net — оркестратор зовёт docker напрямую.)
FROM zcore-rooms-dev

USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends iproute2 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# -tags enet собирает inc3-клиента, когда тот появится; до этого — стаб.
# rooms-бинарь в том же образе — docker-режим оркестратора гоняет сервер
# контейнером в общей сети с loadgen (INPUT-файрвол хоста не пускает
# контейнеры на непублицированные порты шлюза).
RUN CGO_ENABLED=1 go build -tags enet -o /app/loadgen ./cmd/loadgen \
    && CGO_ENABLED=1 go build -tags enet -o /app/rooms ./cmd/rooms

COPY deploy/loadgen/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# LOADGEN_REPORT по умолчанию /out/report.json — монтируется оркестратором.
RUN mkdir -p /out
WORKDIR /out
ENTRYPOINT ["/app/entrypoint.sh"]
