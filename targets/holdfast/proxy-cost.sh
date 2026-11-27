#!/usr/bin/env bash
# What the recording proxy adds to reply latency (spec §5): storms with and without it, in
# pairs whose order alternates, on the same schedule, under the default fake settings and
# under one that answers at once. Restarts the local stack before every run, so stop yours
# first. Needs Postgres and Redis up, Node 26 on PATH and uv. Takes several minutes.
#
#   fnm exec --using=26 -- bash targets/holdfast/proxy-cost.sh
set -euo pipefail
cd "$(dirname "$0")/../.."
HOLDFAST_DIR="${HOLDFAST_DIR:-$HOME/projects/holdfast}"
PAIRS="${PAIRS:-5}"
OUT="${OUT:-runs/proxy-cost-$(date -u +%Y%m%dT%H%M%SZ)}"
export DATABASE_URL="${DATABASE_URL:-postgres://holdfast:holdfast@localhost:5432/holdfast}"
export REDIS_URL="${REDIS_URL:-redis://localhost:6379}"
export OTEL_EXPORTER_OTLP_ENDPOINT=

for port in 3000 3001 3002 4010 4011; do
  if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
    echo "something is already listening on :$port; stop it first" >&2
    exit 1
  fi
done
make build
mkdir -p "$OUT/warmup"
set -m # each stack gets its own process group, so all of it can be stopped
stack=
stop_stack() {
  [ -n "$stack" ] || return 0
  kill -- "-$stack" 2>/dev/null || true
  wait "$stack" 2>/dev/null || true
  stack=
  while curl -s -o /dev/null localhost:3000 || curl -s -o /dev/null localhost:4010; do sleep 0.5; done
}
trap stop_stack EXIT

run_one() { # fake-env-file proxy(true|false)
  local file=$1 proxy=$2 url=
  [ "$proxy" = true ] && url=http://127.0.0.1:4011
  (
    set -a
    . "$file"
    set +a
    FAKE_PAYMENTS_URL="$url" exec bash "$HOLDFAST_DIR/load/stack.sh"
  ) >>"$OUT/stack.log" 2>&1 &
  stack=$!
  for _ in $(seq 60); do
    curl -sf -o /dev/null localhost:3000/api/healthz && curl -sf -o /dev/null localhost:4010/healthz && break
    sleep 1
  done
  # A fresh server is slow at first, so a short storm goes first and is thrown away.
  ./bin/bidstorm -holdfast "$HOLDFAST_DIR" -fake-env "$file" -proxy="$proxy" \
    -duration 3s -auction 2s -out "$OUT/warmup" >/dev/null || true
  # An unusable run is kept: the summary reports it.
  ./bin/bidstorm -holdfast "$HOLDFAST_DIR" -fake-env "$file" -proxy="$proxy" -out "$OUT" |
    grep -E "INVALID|recording:" || true
  stop_stack
}

for file in targets/holdfast/fake.env targets/holdfast/fake-quick.env; do
  for pair in $(seq "$PAIRS"); do
    if [ $((pair % 2)) = 1 ]; then order="true false"; else order="false true"; fi
    for proxy in $order; do
      echo "== $(basename "$file") pair $pair proxy=$proxy"
      run_one "$file" "$proxy"
    done
  done
done
rm -rf "$OUT/warmup"
uv run --project analysis --locked bidcost "$OUT"
