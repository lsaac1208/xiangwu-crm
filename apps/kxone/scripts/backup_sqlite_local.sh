#!/usr/bin/env bash
set -euo pipefail

DB_PATH="${KXONE_DB_PATH:-${DB_NAME:-/var/lib/kxone/kxone.db}}"
BACKUP_DIR="${KXONE_BACKUP_DIR:-/opt/kxone/backups/db}"
RETENTION_DAYS="${KXONE_BACKUP_RETENTION_DAYS:-30}"

if [[ ! -f "$DB_PATH" ]]; then
  echo "SQLite database does not exist: $DB_PATH" >&2
  exit 1
fi
if [[ ! "$RETENTION_DAYS" =~ ^[0-9]+$ ]]; then
  echo "KXONE_BACKUP_RETENTION_DAYS must be a non-negative integer" >&2
  exit 1
fi

umask 077
mkdir -p "$BACKUP_DIR"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup="$BACKUP_DIR/kxone-$stamp.db"
compressed="$backup.gz"
trap 'rm -f "$backup"' EXIT

if command -v python3 >/dev/null 2>&1; then
  python3 - "$DB_PATH" "$backup" <<'PY'
import sqlite3
import sys

source = sqlite3.connect(sys.argv[1])
destination = sqlite3.connect(sys.argv[2])
try:
    source.backup(destination)
    result = destination.execute("PRAGMA integrity_check").fetchone()
    if not result or result[0] != "ok":
        raise RuntimeError(f"integrity_check failed: {result}")
finally:
    destination.close()
    source.close()
PY
elif command -v sqlite3 >/dev/null 2>&1; then
  sqlite3 "$DB_PATH" ".timeout 5000" ".backup '$backup'"
  if [[ "$(sqlite3 "$backup" 'PRAGMA integrity_check;')" != "ok" ]]; then
    echo "SQLite backup integrity_check failed" >&2
    exit 1
  fi
else
  echo "python3 or sqlite3 is required for an online-consistent backup" >&2
  exit 1
fi

gzip -9 "$backup"
chmod 0600 "$compressed"
find "$BACKUP_DIR" -maxdepth 1 -type f -name 'kxone-*.db.gz' -mtime "+$RETENTION_DAYS" -delete
trap - EXIT
echo "$compressed"
