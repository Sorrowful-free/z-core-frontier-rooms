#!/usr/bin/env bash
# Entrypoint loadgen-контейнера (P27 inc2): tc netem на egress, затем loadgen.
#
# Топология: prio root qdisc на $LOADGEN_NETEM_DEV (default eth0), priomap
# весь немаркированный трафик гонит в band 1:1 (clean — без netem-ребёнка).
# fw-фильтры: SO_MARK N → flowid 1:N. Марки ставит сам loadgen-клиент
# (SO_MARK по профилю из LOADGEN_NETEM_SHARE — internal/loadgen/netem.go):
#
#   mark 1 → 1:1 clean (явный фильтр, без netem)
#   mark 2 → 1:2 bad   (netem из LOADGEN_NETEM_BAD,   default «loss 5% delay 150ms 50ms»)
#   mark 3 → 1:3 ugly  (netem из LOADGEN_NETEM_UGLY,  default «loss 20% delay 300ms 150ms reorder 10%»)
#
# Параметры пресетов — строки аргументов `tc qdisc ... netem`, оверрайдятся
# env целиком (LOADGEN_NETEM_BAD/LOADGEN_NETEM_UGLY).
#
# Требует cap_add: NET_ADMIN. Без capability tc не поднимается; если при этом
# задан LOADGEN_NETEM_SHARE — выходим (dial'ы с SO_MARK всё равно упадут на
# EPERM, прогон будет мусором); share не задан — честно работаем unimpaired.
set -euo pipefail

DEV="${LOADGEN_NETEM_DEV:-eth0}"
BAD="${LOADGEN_NETEM_BAD:-loss 5% delay 150ms 50ms}"
UGLY="${LOADGEN_NETEM_UGLY:-loss 20% delay 300ms 150ms reorder 10%}"

if tc qdisc add dev "$DEV" root handle 1: prio priomap 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1; then
  # shellcheck disable=SC2086 # пресеты — строки аргументов netem
  tc qdisc add dev "$DEV" parent 1:2 handle 20: netem $BAD
  # shellcheck disable=SC2086
  tc qdisc add dev "$DEV" parent 1:3 handle 30: netem $UGLY
  tc filter add dev "$DEV" parent 1:0 protocol ip handle 1 fw flowid 1:1
  tc filter add dev "$DEV" parent 1:0 protocol ip handle 2 fw flowid 1:2
  tc filter add dev "$DEV" parent 1:0 protocol ip handle 3 fw flowid 1:3
  echo "[entrypoint] netem up on $DEV: bad='$BAD' ugly='$UGLY'"
else
  if [[ -n "${LOADGEN_NETEM_SHARE:-}" ]]; then
    echo "[entrypoint] FATAL: tc failed (нет NET_ADMIN?), а NETEM_SHARE задан — SO_MARK тоже не встанет" >&2
    exit 1
  fi
  echo "[entrypoint] warn: tc failed — running unimpaired" >&2
fi

exec /app/loadgen "$@"
