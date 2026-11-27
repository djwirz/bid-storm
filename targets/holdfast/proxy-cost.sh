#!/usr/bin/env bash
# What the recording proxy adds to reply latency (spec §5): storms with and without it, in
# pairs whose order alternates, on the same schedule, under the default fake settings and
# under one that answers at once. Restarts the local stack before every run, so stop yours
# first. Needs Postgres and Redis up, Node 26 on PATH and uv. Takes several minutes.
#
#   fnm exec --using=26 -- bash targets/holdfast/proxy-cost.sh
set -euo pipefail
cd "$(dirname "$0")/../.."
PAIRS="${PAIRS:-5}"
OUT="${OUT:-runs/proxy-cost-$(date -u +%Y%m%dT%H%M%SZ)}"
. targets/holdfast/stack-lib.sh

require_free_ports
make build
mkdir -p "$OUT/warmup"
run_one() { # fake-env-file proxy(true|false)
  local file=$1 proxy=$2
  start_stack "$file" "$proxy"
  # A fresh server is slow at first, so a short storm goes first and is thrown away.
  ./bin/bidstorm -holdfast "$HOLDFAST_DIR" -fake-env "$file" -proxy="$proxy" \
    -duration 3s -auction 2s -out "$OUT/warmup" >/dev/null 2>&1 || true
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
