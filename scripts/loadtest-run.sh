#!/usr/bin/env bash
# loadtest-run.sh — оркестратор нагрузочного прогона (P27).
#
# Host-режим (inc1): поднимает cmd/rooms с пресетом .env.loadtest
# (+ сгенерированный ADMISSION_SECRET), дожидается готовности, снимает
# GET /metrics до/после, гоняет cmd/loadgen (LOADGEN_* — из окружения
# вызывающего) и мерджит снимки в report.server_metrics. Сервер глушится
# на выходе.
#
# Использование:
#   LOADGEN_ROOMS=4 LOADGEN_ROOM_MIX="4/4:100" \
#   LOADGEN_TRANSPORT_MIX="ws:100" LOADGEN_DURATION=30s LOADGEN_RECONNECT_RATE=0.2 \
#   ./scripts/loadtest-run.sh
#
# Docker-режим (inc2): LOADTEST_DOCKER=1 — И сервер, и loadgen идут
# контейнерами в общей сети $LOADTEST_DOCKER_NET (default zcore-loadtest):
# host firewall не пускает контейнеры на непублицированные порты шлюза,
# поэтому «loadgen → хост-сервер» не работает; loadgen достаёт сервер по
# network-alias "rooms" (встроенный DNS user-defined сети — оффлайн-ок).
# HTTP/admin/ENet-порты сервера публикуются на 127.0.0.1 — readiness и
# снимки /metrics идут с хоста мимо impair'а. Egress loadgen-контейнера
# размечается SO_MARK → tc netem (entrypoint образа, cap NET_ADMIN).
# Build образа — --network=host (bridge DNS на этой машине мёртв).
#
# Результат: $LOADTEST_OUT_DIR/report.json (default .scratch/loadtest/).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

DOCKER_MODE="${LOADTEST_DOCKER:-0}"
LOADGEN_IMAGE="${LOADGEN_IMAGE:-zcore-loadgen}"
DOCKER_NET="${LOADTEST_DOCKER_NET:-zcore-loadtest}"
SRV_NAME=zcore-rooms-lt
PRESET="${LOADTEST_PRESET:-$REPO_ROOT/.env.loadtest}"
OUT_DIR="${LOADTEST_OUT_DIR:-$REPO_ROOT/.scratch/loadtest}"
mkdir -p "$OUT_DIR"
TMP="$(mktemp -d)"

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [[ "${SERVER_CONTAINER:-0}" == "1" ]]; then
    docker rm -f "$SRV_NAME" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

# Пресет сервера + runtime-секрет (в файл не пишется).
set -a
# shellcheck source=/dev/null
. "$PRESET"
set +a
export ADMISSION_SECRET="${ADMISSION_SECRET:-$(openssl rand -hex 32)}"

# Адреса вида ":3000" → "127.0.0.1:3000" для клиентской стороны.
hostport() { echo "${1/#:/127.0.0.1:}"; }
HTTP_HOSTPORT="$(hostport "${HTTP_ADDR:-:3000}")"
ADMIN_HOSTPORT="$(hostport "${ADMIN_LISTEN_ADDR:-}")"

if [[ "$DOCKER_MODE" == "1" ]]; then
  if ! docker image inspect "$LOADGEN_IMAGE" >/dev/null 2>&1; then
    echo "[loadtest] building image $LOADGEN_IMAGE (--network=host)..."
    docker build --network=host -t "$LOADGEN_IMAGE" \
      -f "$REPO_ROOT/deploy/loadgen/Dockerfile" "$REPO_ROOT"
  fi
  docker network inspect "$DOCKER_NET" >/dev/null 2>&1 || \
    docker network create "$DOCKER_NET" >/dev/null

  # env-file сервера: preset без комментариев + секрет; bind адреса
  # принудительно wildcard — внутри сети сервер слушает eth0 контейнера.
  SRV_ENV="$TMP/server.env"
  grep -vE '^[[:space:]]*(#|$)' "$PRESET" > "$SRV_ENV"
  HTTP_PORT="${HTTP_ADDR:-}"; HTTP_PORT="${HTTP_PORT##*:}"; HTTP_PORT="${HTTP_PORT:-3000}"
  {
    echo "ADMISSION_SECRET=$ADMISSION_SECRET"
    echo "HTTP_ADDR=:${HTTP_PORT}"
  } >> "$SRV_ENV"

  publish=(-p "127.0.0.1:${HTTP_PORT}:${HTTP_PORT}")
  if [[ -n "${ADMIN_LISTEN_ADDR:-}" ]]; then
    ADMIN_PORT="${ADMIN_LISTEN_ADDR##*:}"
    echo "ADMIN_LISTEN_ADDR=:${ADMIN_PORT}" >> "$SRV_ENV"
    publish+=(-p "127.0.0.1:${ADMIN_PORT}:${ADMIN_PORT}")
    ADMIN_HOSTPORT="127.0.0.1:${ADMIN_PORT}"
  fi
  # ENet UDP-порты из пресета — публикация для inc3-клиентов.
  IFS=',' read -ra eports <<< "${ENET_LISTEN_PORTS:-}"
  for p in "${eports[@]}"; do
    [[ -n "${p// }" ]] && publish+=(-p "127.0.0.1:${p}:${p}/udp")
  done

  docker rm -f "$SRV_NAME" >/dev/null 2>&1 || true
  echo "[loadtest] starting rooms container $SRV_NAME (net $DOCKER_NET)..."
  docker run -d --name "$SRV_NAME" --network "$DOCKER_NET" --network-alias rooms \
    --env-file "$SRV_ENV" "${publish[@]}" \
    --entrypoint /app/rooms "$LOADGEN_IMAGE" >/dev/null
  SERVER_CONTAINER=1
  HTTP_HOSTPORT="127.0.0.1:${HTTP_PORT}"

  for _ in $(seq 1 100); do
    if curl -sf "http://$HTTP_HOSTPORT/rooms" >/dev/null 2>&1; then
      break
    fi
    if ! docker inspect -f '{{.State.Running}}' "$SRV_NAME" 2>/dev/null | grep -q true; then
      echo "[loadtest] server container died at startup — log:" >&2
      docker logs --tail 20 "$SRV_NAME" >&2
      exit 1
    fi
    sleep 0.2
  done
  if ! curl -sf "http://$HTTP_HOSTPORT/rooms" >/dev/null 2>&1; then
    echo "[loadtest] server not ready on $HTTP_HOSTPORT — log:" >&2
    docker logs --tail 20 "$SRV_NAME" >&2
    exit 1
  fi
  echo "[loadtest] server up (container $SRV_NAME, http $HTTP_HOSTPORT)"
else
  echo "[loadtest] building server+loadgen..."
  go build -o "$TMP/rooms" ./cmd/rooms
  go build -o "$TMP/loadgen" ./cmd/loadgen

  echo "[loadtest] starting rooms (log: $TMP/server.log)..."
  "$TMP/rooms" >"$TMP/server.log" 2>&1 &
  SERVER_PID=$!

  # readiness: GET /rooms отвечает 200 при поднятом listener'е.
  for _ in $(seq 1 100); do
    if curl -sf "http://$HTTP_HOSTPORT/rooms" >/dev/null 2>&1; then
      break
    fi
    if ! kill -0 "$SERVER_PID" 2>/dev/null; then
      echo "[loadtest] server died at startup — log:" >&2
      tail -20 "$TMP/server.log" >&2
      exit 1
    fi
    sleep 0.2
  done
  if ! curl -sf "http://$HTTP_HOSTPORT/rooms" >/dev/null 2>&1; then
    echo "[loadtest] server not ready on $HTTP_HOSTPORT — log:" >&2
    tail -20 "$TMP/server.log" >&2
    exit 1
  fi
  echo "[loadtest] server up (pid $SERVER_PID, http $HTTP_HOSTPORT)"
fi

# Снимок /metrics до прогона (admin listener — опционален в пресете).
METRICS_BEFORE="$TMP/metrics_before.txt"
METRICS_AFTER="$TMP/metrics_after.txt"
: >"$METRICS_BEFORE"
: >"$METRICS_AFTER"
if [[ -n "${ADMIN_LISTEN_ADDR:-}" ]]; then
  curl -sf "http://$ADMIN_HOSTPORT/metrics" >"$METRICS_BEFORE" || \
    echo "[loadtest] warn: metrics scrape before failed ($ADMIN_HOSTPORT)" >&2
fi

if [[ "$DOCKER_MODE" == "1" ]]; then
  # In-net адреса: сервер по alias "rooms", порты контейнерные.
  export LOADGEN_HTTP_ADDR="rooms:${HTTP_PORT}"
  export LOADGEN_WS_ADDR="rooms:${HTTP_PORT}"
  [[ -n "${ADMIN_LISTEN_ADDR:-}" ]] && export LOADGEN_ADMIN_ADDR="rooms:${ADMIN_PORT}"
  export LOADGEN_REPORT=/out/report.json

  echo "[loadtest] running loadgen container (report: $OUT_DIR/report.json)..."
  docker_args=(--rm --cap-add NET_ADMIN --network "$DOCKER_NET" -v "$OUT_DIR:/out")
  while IFS='=' read -r name; do
    case "$name" in
      LOADGEN_*) docker_args+=(-e "$name") ;;
    esac
  done < <(env | cut -d= -f1)
  docker run "${docker_args[@]}" "$LOADGEN_IMAGE"
  LOADGEN_REPORT="$OUT_DIR/report.json"
else
  export LOADGEN_HTTP_ADDR="${LOADGEN_HTTP_ADDR:-$HTTP_HOSTPORT}"
  export LOADGEN_WS_ADDR="${LOADGEN_WS_ADDR:-$LOADGEN_HTTP_ADDR}"
  export LOADGEN_ADMIN_ADDR="${LOADGEN_ADMIN_ADDR:-$ADMIN_HOSTPORT}"
  export LOADGEN_REPORT="${LOADGEN_REPORT:-$OUT_DIR/report.json}"

  echo "[loadtest] running loadgen (report: $LOADGEN_REPORT)..."
  "$TMP/loadgen"
fi

if [[ -n "${ADMIN_LISTEN_ADDR:-}" ]]; then
  curl -sf "http://$ADMIN_HOSTPORT/metrics" >"$METRICS_AFTER" || \
    echo "[loadtest] warn: metrics scrape after failed ($ADMIN_HOSTPORT)" >&2
fi

# Снимки до/после → report.server_metrics.
if [[ -f "$LOADGEN_REPORT" ]]; then
  jq --rawfile before "$METRICS_BEFORE" --rawfile after "$METRICS_AFTER" \
    '.server_metrics = {before: $before, after: $after}' \
    "$LOADGEN_REPORT" >"$LOADGEN_REPORT.tmp" && mv "$LOADGEN_REPORT.tmp" "$LOADGEN_REPORT"
  echo "[loadtest] report: $LOADGEN_REPORT"
else
  echo "[loadtest] warn: report file not found at $LOADGEN_REPORT" >&2
fi

echo "[loadtest] done"
