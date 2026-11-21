#!/usr/bin/env bash
set -euo pipefail
umask 077
export LC_ALL=C LANG=C

: "${KXONE_DB:?KXONE_DB is required}"
: "${GATEWAY_DB:?GATEWAY_DB is required}"
: "${SITE_CURRENT:?SITE_CURRENT is required}"
: "${BACKUP_DIR:?BACKUP_DIR is required}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-30}"

for command in sqlite3 tar; do command -v "$command" >/dev/null || { echo "missing command: $command" >&2; exit 1; }; done
if command -v sha256sum >/dev/null 2>&1; then
  checksum=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  checksum=(shasum -a 256)
else
  echo 'missing SHA-256 command: sha256sum or shasum' >&2
  exit 1
fi
[[ -f "$KXONE_DB" ]] || { echo "missing KXOne DB: $KXONE_DB" >&2; exit 1; }
[[ -f "$GATEWAY_DB" ]] || { echo "missing gateway DB: $GATEWAY_DB" >&2; exit 1; }
[[ -e "$SITE_CURRENT" ]] || { echo "missing site current path: $SITE_CURRENT" >&2; exit 1; }

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$BACKUP_DIR"
staging="$(mktemp -d "$BACKUP_DIR/.staging-${timestamp}-XXXXXX")"
target="$BACKUP_DIR/$timestamp"
trap 'rm -rf "$staging"' EXIT

sqlite3 "$KXONE_DB" ".timeout 10000" ".backup '$staging/kxone.db'"
sqlite3 "$GATEWAY_DB" ".timeout 10000" ".backup '$staging/lead-gateway.db'"
site_release="$(cd "$(dirname "$SITE_CURRENT")" && cd "$(readlink "$SITE_CURRENT" 2>/dev/null || basename "$SITE_CURRENT")" && pwd -P)"
tar -C "$site_release" -czf "$staging/site-release.tar.gz" .
printf 'created_at_utc=%s\nsite_release=%s\n' "$timestamp" "$site_release" >"$staging/manifest.txt"
(cd "$staging" && "${checksum[@]}" kxone.db lead-gateway.db site-release.tar.gz manifest.txt >SHA256SUMS)
mv "$staging" "$target"
trap - EXIT

"$(dirname "$0")/verify-backup.sh" "$target"
find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -type d -name '20????????T??????Z' -mtime "+$BACKUP_RETENTION_DAYS" -exec rm -rf {} +
echo "backup created: $target"
