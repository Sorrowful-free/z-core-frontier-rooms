#!/usr/bin/env bash
# loadtest-run.sh — host-режим оркестратора нагрузочного прогона (P27, inc 1).
#
# Поднимает cmd/rooms с пресетом .env.loadtest (+ сгенерированный
# ADMISSION_SECRET), дожидается готовности, снимает GET /metrics до/после,
# гоняет cmd/loadgen (LOADGEN_* — из окружения вызывающего) и мерджит снимки
# в report.server_metrics. Сервер глушится на выходе.
#
# Использование:
#   LOADGEN_ROOMS=4 LOADGEN_ROOM_MIX="4/4:100" LOADGEN_POLICY_MIX="state:50,auth:50" \
#   LOADGEN_TRANSPORT_MIX="ws:100" LOADGEN_DURATION=30s LOADGEN_RECONNECT_RATE=0.2 \
#   ./scripts/loadtest-run.sh
#
# Результат: $LOADTEST_OUT_DIR/report.json (default .scratch/loadtest/).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

PRESET="${LOADTEST_PRESET:-$REPO_ROOT/.env.loadtest}"
OUT_DIR="${LOADTEST_OUT_DIR:-$REPO_ROOT/.scratch/loadtest}"
mkdir -p "$OUT_DIR"
TMP="$(mktemp -d)"

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
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

# Снимок /metrics до прогона (admin listener — опционален в пресете).
METRICS_BEFORE="$TMP/metrics_before.txt"
METRICS_AFTER="$TMP/metrics_after.txt"
: >"$METRICS_BEFORE"
: >"$METRICS_AFTER"
if [[ -n "${ADMIN_LISTEN_ADDR:-}" ]]; then
  curl -sf "http://$ADMIN_HOSTPORT/metrics" >"$METRICS_BEFORE" || \
    echo "[loadtest] warn: metrics scrape before failed ($ADMIN_HOSTPORT)" >&2
fi

export LOADGEN_HTTP_ADDR="${LOADGEN_HTTP_ADDR:-$HTTP_HOSTPORT}"
export LOADGEN_WS_ADDR="${LOADGEN_WS_ADDR:-$LOADGEN_HTTP_ADDR}"
export LOADGEN_ADMIN_ADDR="${LOADGEN_ADMIN_ADDR:-$ADMIN_HOSTPORT}"
export LOADGEN_REPORT="${LOADGEN_REPORT:-$OUT_DIR/report.json}"

echo "[loadtest] running loadgen (report: $LOADGEN_REPORT)..."
"$TMP/loadgen"

if [[ -n "${ADMIN_LISTEN_ADDR:-}" ]]; then
  curl -sf "http://$ADMIN_HOSTPORT/metrics" >"$METRICS_AFTER" || \
    echo "[loadtest] warn: metrics scrape after failed" >&2
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

echo "[loadtest] done (server log: $TMP/server.log)"
