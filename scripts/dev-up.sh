#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -f "$ROOT/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$ROOT/.env"
  set +a
fi

RUNTIME_DIR="${RUNTIME_DIR:-.runtime}"
[[ "$RUNTIME_DIR" == /* ]] || RUNTIME_DIR="$ROOT/$RUNTIME_DIR"
PID_DIR="$RUNTIME_DIR/pids"
LOG_DIR="$RUNTIME_DIR/logs"
DATA_DIR="$RUNTIME_DIR/data"
mkdir -p "$PID_DIR" "$LOG_DIR" "$DATA_DIR" "$RUNTIME_DIR/cache"

REDIS_HOST="${REDIS_HOST:-127.0.0.1}"
REDIS_PORT="${REDIS_PORT:-6380}"
REDIS_DB="${REDIS_DB:-0}"
KXONE_HOST="${KXONE_HOST:-127.0.0.1}"
KXONE_PORT="${KXONE_PORT:-8080}"
H5_PORT="${H5_PORT:-5173}"

for command in go node npm redis-server curl; do
  command -v "$command" >/dev/null 2>&1 || { echo "missing required command: $command" >&2; exit 1; }
done

(cd "$ROOT/apps/kxone" && go build -o "$RUNTIME_DIR/cache/kxone-server" ./cmd/server && go build -o "$RUNTIME_DIR/cache/kxone-worker" ./cmd/worker)

start_service() {
  local name="$1"
  shift
  local pid_file="$PID_DIR/$name.pid"
  if [[ -f "$pid_file" ]] && kill -0 "$(cat "$pid_file")" 2>/dev/null; then
    echo "$name is already running (pid $(cat "$pid_file"))"
    return
  fi
  nohup "$@" </dev/null >"$LOG_DIR/$name.log" 2>&1 &
  local pid=$!
  echo "$pid" >"$pid_file"
  echo "started $name (pid $pid)"
}

cleanup_on_error() {
  local exit_code=$?
  if [[ $exit_code -ne 0 ]]; then
    echo "development startup failed; stopping services started by this workspace" >&2
    "$ROOT/scripts/dev-down.sh" >/dev/null 2>&1 || true
  fi
  exit "$exit_code"
}
trap cleanup_on_error EXIT

start_service redis env LC_ALL=C LANG=C redis-server \
  --bind "$REDIS_HOST" --port "$REDIS_PORT" --dir "$DATA_DIR" \
  --dbfilename redis.rdb --appendonly no --save ""

start_service kxone-server env \
  SERVER_ENV=dev SERVER_HOST="$KXONE_HOST" SERVER_PORT="$KXONE_PORT" \
  SERVER_PUBLIC_BASE_URL="${SERVER_PUBLIC_BASE_URL:-http://$KXONE_HOST:$KXONE_PORT}" \
  CORS_ALLOWED_ORIGINS="${CORS_ALLOWED_ORIGINS:-http://127.0.0.1:$H5_PORT}" \
  DB_DRIVER=sqlite DB_NAME="$DATA_DIR/kxone.db" DB_MAX_CONNS=4 ENABLE_AUTO_MIGRATE=true \
  MIGRATIONS_DIR="$ROOT/apps/kxone/migrations" REDIS_HOST="$REDIS_HOST" REDIS_PORT="$REDIS_PORT" REDIS_DB="$REDIS_DB" \
  WORKER_METRICS_ADDR="127.0.0.1:${KXONE_WORKER_METRICS_PORT:-9091}" \
  AD_LEAD_DEFAULT_MANAGER_USERID="${AD_LEAD_DEFAULT_MANAGER_USERID:-dev-manager}" \
  AD_LEAD_WEBHOOK_SECRET="${AD_LEAD_WEBHOOK_SECRET:-dev-ad-lead-webhook-secret}" \
  AD_LEAD_GATEWAY_ENABLED="${AD_LEAD_GATEWAY_ENABLED:-false}" \
  AD_LEAD_GATEWAY_HMAC_SECRET="${AD_LEAD_GATEWAY_HMAC_SECRET:-}" \
  "$RUNTIME_DIR/cache/kxone-server"

for _ in {1..240}; do
  curl -fsS "http://$KXONE_HOST:$KXONE_PORT/health" >/dev/null 2>&1 && break
  sleep 0.25
done
curl -fsS "http://$KXONE_HOST:$KXONE_PORT/health" >/dev/null || { echo "KXOne failed to become healthy; see $LOG_DIR/kxone-server.log" >&2; exit 1; }

start_service kxone-worker env \
  SERVER_ENV=dev DB_DRIVER=sqlite DB_NAME="$DATA_DIR/kxone.db" DB_MAX_CONNS=4 \
  MIGRATIONS_DIR="$ROOT/apps/kxone/migrations" REDIS_HOST="$REDIS_HOST" REDIS_PORT="$REDIS_PORT" REDIS_DB="$REDIS_DB" \
  WORKER_METRICS_ADDR="127.0.0.1:${KXONE_WORKER_METRICS_PORT:-9091}" \
  "$RUNTIME_DIR/cache/kxone-worker"

start_service kxone-h5 env \
  VITE_API_PROXY_TARGET="${VITE_API_PROXY_TARGET:-http://$KXONE_HOST:$KXONE_PORT}" \
  VITE_DEV_TOKEN_SECRET="${VITE_DEV_TOKEN_SECRET:-}" \
  bash -c 'cd "$1" && exec npm run dev -- --host "$2" --port "$3"' _ "$ROOT/apps/kxone/web" "${H5_HOST:-127.0.0.1}" "$H5_PORT"

echo "logs: $LOG_DIR"
for _ in {1..60}; do
  if "$ROOT/scripts/dev-status.sh" >/dev/null 2>&1; then
    trap - EXIT
    "$ROOT/scripts/dev-status.sh"
    exit 0
  fi
  sleep 0.5
done

"$ROOT/scripts/dev-status.sh" || true
echo "development stack failed to become ready; inspect $LOG_DIR" >&2
exit 1
