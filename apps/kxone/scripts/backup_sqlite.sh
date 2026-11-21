#!/usr/bin/env bash
set -euo pipefail

: "${KXONE_SERVER_HOST:?KXONE_SERVER_HOST is required}"
: "${KXONE_SERVER_USER:?KXONE_SERVER_USER is required}"

SERVER_HOST="$KXONE_SERVER_HOST"
SERVER_USER="$KXONE_SERVER_USER"
SSH_KEY="${KXONE_SSH_KEY:-}"
REMOTE_DB="${KXONE_REMOTE_DB:-/var/lib/kxone/kxone.db}"
REMOTE_BACKUP_DIR="${KXONE_REMOTE_BACKUP_DIR:-/var/backups/kxone}"
LOCAL_BACKUP_DIR="${KXONE_LOCAL_BACKUP_DIR:-./backups}"
SSH_TARGET="${SERVER_USER}@${SERVER_HOST}"
SSH_ARGS=()
if [[ -n "$SSH_KEY" ]]; then
  SSH_ARGS=(-i "$SSH_KEY")
fi

mkdir -p "$LOCAL_BACKUP_DIR"

remote_file="$(ssh "${SSH_ARGS[@]}" "$SSH_TARGET" "set -euo pipefail
test -f '$REMOTE_DB'
mkdir -p '$REMOTE_BACKUP_DIR'
stamp=\$(date +%Y%m%d%H%M%S)
backup='$REMOTE_BACKUP_DIR/kxone-'\$stamp'.db'
if command -v sqlite3 >/dev/null 2>&1; then
  sqlite3 '$REMOTE_DB' \".backup '\$backup'\"
elif command -v python3 >/dev/null 2>&1; then
  python3 - '$REMOTE_DB' \"\$backup\" <<'PY'
import sqlite3
import sys

source = sqlite3.connect(sys.argv[1])
destination = sqlite3.connect(sys.argv[2])
try:
    source.backup(destination)
finally:
    destination.close()
    source.close()
PY
else
  echo 'sqlite3 or Python sqlite3 support is required for an online-consistent backup' >&2
  exit 1
fi
gzip -f \"\$backup\"
echo \"\$backup.gz\"
")"

scp "${SSH_ARGS[@]}" "$SSH_TARGET:$remote_file" "$LOCAL_BACKUP_DIR/"
echo "Backup saved to $LOCAL_BACKUP_DIR/$(basename "$remote_file")"
