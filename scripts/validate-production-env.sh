#!/usr/bin/env bash
set -euo pipefail

service="${1:-}"
fail() { echo "production environment invalid: $*" >&2; exit 1; }
require() { local name="$1"; [[ -n "${!name:-}" ]] || fail "$name is required for $service"; }
secret() {
  local name="$1" minimum="${2:-32}" value="${!1:-}"
  require "$name"
  [[ ${#value} -ge $minimum ]] || fail "$name must contain at least $minimum characters"
  [[ "$value" != replace-with-* && "$value" != dev-* ]] || fail "$name still contains an example value"
}
absolute() { local name="$1"; require "$name"; [[ "${!name}" == /* ]] || fail "$name must be an absolute path"; }

case "$service" in
  kxone|worker)
    [[ "${SERVER_ENV:-}" == prod ]] || fail "SERVER_ENV must be prod"
    absolute DB_NAME
    secret JWT_SECRET
    secret FIELD_ENCRYPTION_KEY
    secret MOBILE_HASH_KEY
    if [[ "${AD_LEAD_GATEWAY_ENABLED:-false}" == true ]]; then secret AD_LEAD_GATEWAY_HMAC_SECRET; fi
    if [[ "${CMS_ENABLED:-false}" == true ]]; then
      fail "CMS has been removed; CMS_ENABLED must not be true"
    fi
    ;;
  gateway)
    [[ "${NODE_ENV:-}" == production ]] || fail "NODE_ENV must be production"
    absolute DATABASE_PATH
    case "${LEAD_PIPELINE_MODE:-}" in
      legacy) ;;
      dual-write|kxone-only) secret KXONE_GATEWAY_HMAC_SECRET; secret RECONCILIATION_HASH_KEY ;;
      *) fail "LEAD_PIPELINE_MODE must be legacy, dual-write or kxone-only" ;;
    esac
    ;;
  *) fail "usage: $0 kxone|worker|gateway" ;;
esac

echo "$service production environment validated"
