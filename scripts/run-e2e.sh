#!/usr/bin/env bash

set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose_file="$repo_root/tests/e2e/compose.yml"
mode="${1:-${E2E_MODE:-quick}}"
project_name="${E2E_PROJECT_NAME:-xiangwu-pipeline-e2e}"
keep_stack="${E2E_KEEP_STACK:-0}"
report_dir="${E2E_REPORT_DIR:-$repo_root/tests/e2e/reports}"
report_file="$report_dir/$mode.json"
failure_log="$report_dir/$mode-logs.txt"
compose=(docker compose --project-name "$project_name" --file "$compose_file")

case "$mode" in
  quick|nightly) ;;
  *)
    printf 'usage: %s [quick|nightly]\n' "$0" >&2
    exit 64
    ;;
esac
export E2E_MODE="$mode"
mkdir -p "$report_dir"
rm -f "$report_file" "$failure_log"

sanitize_logs() {
  sed -E \
    -e 's/([0-9]{3})[0-9]{4}([0-9]{4})/\1****\2/g' \
    -e 's/((corpsecret|access_token|token|secret|key)=)[^&[:space:]]+/\1***REDACTED***/gi'
}

copy_report() {
  "${compose[@]}" cp e2e-runner:/tmp/e2e-report.json "$report_file" >/dev/null 2>&1 || return 1
  chmod 600 "$report_file"
}

retry_network_step() {
  local attempt=1
  local max_attempts=3

  until "$@"; do
    if (( attempt >= max_attempts )); then
      return 1
    fi
    printf 'E2E dependency preparation failed (attempt %d/%d); retrying...\n' \
      "$attempt" "$max_attempts" >&2
    sleep $((attempt * 5))
    ((attempt += 1))
  done
}

finish() {
  local status=$?
  local raw_log
  trap - EXIT

  if ! copy_report; then
    if (( status == 0 )); then
      status=1
    fi
    printf '{"mode":"%s","status":"failed","error":"runner report unavailable"}\n' "$mode" > "$report_file"
    chmod 600 "$report_file"
  fi

  if (( status != 0 )); then
    "${compose[@]}" ps || true
    raw_log="$(mktemp)"
    "${compose[@]}" logs --no-color --tail=500 > "$raw_log" 2>&1 || true
    sanitize_logs < "$raw_log" > "$failure_log"
    tail -250 "$failure_log" >&2 || true
    rm -f "$raw_log"
  fi

  if [[ "$keep_stack" == "1" ]]; then
    printf 'E2E stack preserved: project=%s mode=%s\n' "$project_name" "$mode"
  else
    "${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
  fi

  exit "$status"
}
trap finish EXIT

command -v docker >/dev/null 2>&1 || {
  printf 'docker is required to run the E2E stack\n' >&2
  exit 127
}
docker info >/dev/null
docker compose version >/dev/null

"${compose[@]}" config --quiet
"${compose[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
retry_network_step "${compose[@]}" pull --ignore-buildable
retry_network_step "${compose[@]}" build
"${compose[@]}" up --no-build --abort-on-container-exit --exit-code-from e2e-runner e2e-runner
