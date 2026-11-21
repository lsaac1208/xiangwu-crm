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

terminate_tree() {
  local pid="$1" child
  for child in $(pgrep -P "$pid" 2>/dev/null || true); do
    terminate_tree "$child"
  done
  kill "$pid" 2>/dev/null || true
}

for name in kxone-h5 kxone-worker kxone-server redis; do
  pid_file="$PID_DIR/$name.pid"
  [[ -f "$pid_file" ]] || continue
  pid="$(cat "$pid_file")"
  if kill -0 "$pid" 2>/dev/null; then
    terminate_tree "$pid"
    echo "stopping $name (pid $pid)"
  fi
done

for _ in {1..40}; do
  running=false
  for pid_file in "$PID_DIR"/*.pid; do
    [[ -e "$pid_file" ]] || continue
    kill -0 "$(cat "$pid_file")" 2>/dev/null && running=true
  done
  [[ "$running" == false ]] && break
  sleep 0.25
done

for pid_file in "$PID_DIR"/*.pid; do
  [[ -e "$pid_file" ]] || continue
  pid="$(cat "$pid_file")"
  kill -0 "$pid" 2>/dev/null && kill -KILL "$pid" 2>/dev/null || true
  rm "$pid_file"
done
echo "development stack stopped"
