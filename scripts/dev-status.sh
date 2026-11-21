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

status=0
check() {
  local name="$1" url="$2"
  if curl -fsS --max-time 2 "$url" >/dev/null 2>&1; then
    printf '%-16s %s\n' "$name" "healthy  $url"
  else
    printf '%-16s %s\n' "$name" "unavailable  $url"
    status=1
  fi
}

check kxone "http://${KXONE_HOST:-127.0.0.1}:${KXONE_PORT:-8080}/health"
check h5 "http://${H5_HOST:-127.0.0.1}:${H5_PORT:-5173}/"
exit "$status"
