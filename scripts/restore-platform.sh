#!/usr/bin/env bash
set -euo pipefail
umask 077
export LC_ALL=C LANG=C

backup="${1:?usage: $0 BACKUP_DIRECTORY [ISOLATED_DESTINATION]}"
destination="${2:-/tmp/xiangwu-restore-$(date -u +%Y%m%dT%H%M%SZ)}"
[[ ! -e "$destination" ]] || { echo "restore destination already exists: $destination" >&2; exit 1; }
"$(dirname "$0")/verify-backup.sh" "$backup"
mkdir -p "$destination/databases" "$destination/site"
cp "$backup/kxone.db" "$destination/databases/kxone.db"
cp "$backup/lead-gateway.db" "$destination/databases/lead-gateway.db"
tar -C "$destination/site" -xzf "$backup/site-release.tar.gz"
cp "$backup/manifest.txt" "$destination/backup-manifest.txt"
echo "backup restored into isolated destination: $destination"
echo "production data was not modified"
