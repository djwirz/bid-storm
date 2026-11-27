# Shared by the scripts that restart the local Holdfast stack between runs (proxy-cost.sh,
# sweep.sh). Source it from the repository root after setting OUT.
HOLDFAST_DIR="${HOLDFAST_DIR:-$HOME/projects/holdfast}"
export DATABASE_URL="${DATABASE_URL:-postgres://holdfast:holdfast@localhost:5432/holdfast}"
export REDIS_URL="${REDIS_URL:-redis://localhost:6379}"
export OTEL_EXPORTER_OTLP_ENDPOINT=

require_free_ports() {
  local port
  for port in 3000 3001 3002 4010 4011; do
    if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
      echo "something is already listening on :$port; stop it first" >&2
      exit 1
    fi
  done
}

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

start_stack() { # fake-env-file proxy(true|false)
  local file=$1 proxy=$2
  (
    set -a
    . "$file"
    set +a
    # Without the proxy the variable must be unset, not empty: Holdfast reads an empty one as
    # "no shared fake" and quietly uses a private one with its own wait and no faults.
    if [ "$proxy" = true ]; then
      export FAKE_PAYMENTS_URL=http://127.0.0.1:4011
    else
      unset FAKE_PAYMENTS_URL
    fi
    exec bash "$HOLDFAST_DIR/load/stack.sh"
  ) >>"$OUT/stack.log" 2>&1 &
  stack=$!
  local _
  for _ in $(seq 60); do
    curl -sf -o /dev/null localhost:3000/api/healthz && curl -sf -o /dev/null localhost:4010/healthz && return 0
    sleep 1
  done
  echo "the stack did not come up; see $OUT/stack.log" >&2
  return 1
}
