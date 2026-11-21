#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C LANG=C

backup="${1:?usage: $0 BACKUP_DIRECTORY}"
[[ -d "$backup" ]] || { echo "backup directory not found: $backup" >&2; exit 1; }
for file in kxone.db lead-gateway.db site-release.tar.gz manifest.txt SHA256SUMS; do
  [[ -f "$backup/$file" ]] || { echo "backup file missing: $file" >&2; exit 1; }
done
if command -v sha256sum >/dev/null 2>&1; then
  checksum=(sha256sum -c)
elif command -v shasum >/dev/null 2>&1; then
  checksum=(shasum -a 256 -c)
else
  echo 'missing SHA-256 command: sha256sum or shasum' >&2
  exit 1
fi
(cd "$backup" && "${checksum[@]}" SHA256SUMS)
[[ "$(sqlite3 "$backup/kxone.db" 'PRAGMA integrity_check;')" == ok ]]
[[ "$(sqlite3 "$backup/lead-gateway.db" 'PRAGMA integrity_check;')" == ok ]]
tar -tzf "$backup/site-release.tar.gz" >/dev/null
echo "backup verified: $backup"
