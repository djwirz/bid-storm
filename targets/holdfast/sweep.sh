#!/usr/bin/env bash
# Sweeps over request rate, hold delay and decline rate (spec §7, slice 2d), one variable at a
# time from a base setting, REPEATS times each with seeds 1 to REPEATS. Settings run round by
# round in a shuffled order that is recorded, so drift doesn't line up with a variable.
# Restarts the local stack before every run, so stop yours first. Needs Postgres and Redis up,
# Node 26 on PATH, jq and uv. The full sweep takes about an hour: leave the machine alone.
#
#   fnm exec --using=26 -- bash targets/holdfast/sweep.sh
#   SETTINGS="50 150-500 0.03;10 0 0.03" REPEATS=1 ...   # a trial
set -euo pipefail
cd "$(dirname "$0")/../.."
REPEATS="${REPEATS:-5}"
OUT="${OUT:-runs/sweep-$(date -u +%Y%m%dT%H%M%SZ)}"
# "rate hold-delay-ms decline-rate". The first is the base.
SETTINGS="${SETTINGS:-50 150-500 0.03;10 150-500 0.03;25 150-500 0.03;100 150-500 0.03;200 150-500 0.03;300 150-500 0.03;50 0 0.03;50 50-150 0.03;50 500-1500 0.03;50 150-500 0;50 150-500 0.2;50 150-500 0.5}"
. targets/holdfast/stack-lib.sh

require_free_ports
make build
mkdir -p "$OUT/warmup" "$OUT/env"
load() { uptime | sed -E 's/.*load averages?: ([0-9.]+).*/\1/'; }

order=0
for repeat in $(seq "$REPEATS"); do
  # Shuffled by the round number, so the order can be repeated.
  shuffled=$(printf '%s' "$SETTINGS" | python3 -I -c '
import random, sys
settings = sys.stdin.read().split(";")
random.Random(int(sys.argv[1])).shuffle(settings)
print("\n".join(settings))' "$repeat")
  while read -r rate hold decline; do
    order=$((order + 1))
    file="$OUT/env/hold-$hold-decline-$decline.env"
    sed -E "s/^FAKE_LATENCY_MS=.*/FAKE_LATENCY_MS=$hold/; s/^FAKE_DECLINE_RATE=.*/FAKE_DECLINE_RATE=$decline/" \
      targets/holdfast/fake.env >"$file"
    echo "== run $order: round $repeat, rate $rate, hold $hold ms, decline $decline"
    start_stack "$file" true
    # A fresh server is slow at first: a short storm goes first and is thrown away, without
    # waiting for its stragglers.
    ./bin/bidstorm -holdfast "$HOLDFAST_DIR" -fake-env "$file" -duration 3s -auction 2s -drain-timeout 1s \
      -out "$OUT/warmup" >/dev/null 2>&1 || true
    before=$(load)
    # An unusable run is kept: the summary shows it.
    output=$(./bin/bidstorm -holdfast "$HOLDFAST_DIR" -fake-env "$file" -rate "$rate" -seed "$repeat" \
      -out "$OUT" </dev/null 2>&1) || true
    echo "$output" | grep -E "INVALID|recording:" || true
    run=$(echo "$output" | sed -n 's/^recording: //p')
    if [ -n "$run" ]; then
      jq -n --argjson order "$order" --argjson repeat "$repeat" --argjson rate "$rate" --arg hold "$hold" \
        --argjson decline "$decline" --argjson before "$before" --argjson after "$(load)" \
        '{order: $order, repeat: $repeat, rate: $rate, hold_ms: $hold, decline: $decline,
          load_before: $before, load_after: $after}' >"$run/sweep.json"
    else
      echo "NO RECORDING: $output" | tail -5
    fi
    stop_stack
  done <<<"$shuffled"
done
rm -rf "$OUT/warmup"
echo "done: $OUT"
