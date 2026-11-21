#!/usr/bin/env bash
set -euo pipefail

KXONE_HEALTH_URL="${KXONE_HEALTH_URL:-http://127.0.0.1:18081/health}"
GATEWAY_READY_URL="${GATEWAY_READY_URL:-http://127.0.0.1:3000/readyz}"
SITE_HEALTH_URL="${SITE_HEALTH_URL:-https://www.example.com/}"

check() {
  local name="$1" url="$2"
  curl --fail --silent --show-error --max-time 8 "$url" >/dev/null
  printf '%-14s healthy  %s\n' "$name" "$url"
}

check kxone "$KXONE_HEALTH_URL"
check gateway "$GATEWAY_READY_URL"
check site "$SITE_HEALTH_URL"
