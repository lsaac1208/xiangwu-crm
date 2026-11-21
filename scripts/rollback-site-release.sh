#!/usr/bin/env bash
set -euo pipefail

release_id="${1:?usage: $0 RELEASE_ID [--apply]}"
mode="${2:---check}"
releases_dir="${CMS_RELEASES_DIR:-/var/lib/xiangwu/site/releases}"
current_link="${CMS_CURRENT_LINK:-/var/lib/xiangwu/site/current}"
[[ "$release_id" =~ ^[A-Za-z0-9._-]+$ ]] || { echo 'invalid Release ID' >&2; exit 1; }
release="$releases_dir/$release_id"
[[ -d "$release" && -f "$release/index.html" ]] || { echo "valid built Release not found: $release" >&2; exit 1; }
[[ ! -e "$current_link" || -L "$current_link" ]] || { echo "current path is not a symlink: $current_link" >&2; exit 1; }

echo "current: $(readlink "$current_link" 2>/dev/null || echo '<none>')"
echo "target:  $release"
if [[ "$mode" == --check ]]; then
  echo 'rollback validated; no symlink was changed'
  exit 0
fi
[[ "$mode" == --apply ]] || { echo "usage: $0 RELEASE_ID [--check|--apply]" >&2; exit 1; }

mkdir -p "$(dirname "$current_link")"
ln -sfn "$release" "$current_link"
echo "site current switched to Release $release_id"
