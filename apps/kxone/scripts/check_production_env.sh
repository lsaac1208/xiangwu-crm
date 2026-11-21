#!/usr/bin/env bash
set -euo pipefail

MODE="${1:-server}"
if [[ "$MODE" != "server" && "$MODE" != "worker" ]]; then
  echo "usage: check_production_env.sh [server|worker]" >&2
  exit 2
fi

fail() {
  echo "production configuration error: $*" >&2
  exit 1
}

require_value() {
	local name="$1"
	local value="${!name:-}"
	local lower_value
	[[ -n "${value//[[:space:]]/}" ]] || fail "$name is required"
	lower_value="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')"
	case "$lower_value" in
    *change-me*|*replace-with*|*example.com*) fail "$name still contains a placeholder" ;;
  esac
}

require_absolute_path() {
  local name="$1"
  local value="${!name:-}"
  [[ "$value" == /* ]] || fail "$name must be an absolute path"
}

[[ "${SERVER_ENV:-}" == "prod" ]] || fail "SERVER_ENV must be prod"
case "${SERVER_HOST:-}" in
  127.0.0.1|localhost|::1) ;;
  *) fail "SERVER_HOST must bind to a loopback address behind Nginx" ;;
esac

for name in SERVER_PUBLIC_BASE_URL JWT_SECRET FIELD_ENCRYPTION_KEY MOBILE_HASH_KEY AD_LEAD_DEFAULT_MANAGER_USERID AD_LEAD_WEBHOOK_SECRET; do
  require_value "$name"
done

if [[ "${DB_DRIVER:-}" == "sqlite" ]]; then
  require_absolute_path DB_NAME
  mkdir -p "$(dirname "$DB_NAME")"
  [[ -w "$(dirname "$DB_NAME")" ]] || fail "SQLite directory is not writable: $(dirname "$DB_NAME")"
fi

if [[ "${CMS_ENABLED:-false}" == "true" ]]; then
  fail "CMS has been removed; CMS_ENABLED must not be true"
fi

echo "production configuration check passed for $MODE"
