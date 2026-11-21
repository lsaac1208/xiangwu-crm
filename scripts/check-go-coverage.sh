#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
profile="${1:-$ROOT/apps/kxone/coverage.out}"
threshold_file="$ROOT/quality/go-coverage.env"

[[ -f "$profile" ]] || { echo "Go coverage profile not found: $profile" >&2; exit 1; }
[[ -f "$threshold_file" ]] || { echo "Go coverage threshold file not found: $threshold_file" >&2; exit 1; }

# shellcheck disable=SC1090
source "$threshold_file"
minimum="${GO_MIN_TOTAL:?GO_MIN_TOTAL is required}"
profile="$(cd "$(dirname "$profile")" && pwd)/$(basename "$profile")"
total="$(cd "$ROOT/apps/kxone" && go tool cover -func="$profile" | awk '/^total:/ {gsub(/%/, "", $3); print $3}')"
[[ -n "$total" ]] || { echo "Unable to read total Go coverage from $profile" >&2; exit 1; }

awk -v actual="$total" -v minimum="$minimum" 'BEGIN {
  printf "Go statement coverage: %.1f%% (minimum %.1f%%)\n", actual, minimum
  exit(actual + 0.0001 < minimum)
}'
