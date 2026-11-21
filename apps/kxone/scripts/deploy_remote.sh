#!/usr/bin/env bash
# Invoked by deploy_kxone.sh with positional, shell-quoted arguments. No secrets in arguments.
set -Eeuo pipefail
remote_root=$1; database=$2; env_file=$3; backup_dir=$4; go_binary=$5
source_archive=$6; web_archive=$7; public_url=$8; revision=$9
# Select the predecessor only after acquiring the shared publication lock.
exec 9>"$remote_root/deploy.lock"
flock -n 9
stamp=$(date -u +%Y%m%dT%H%M%SZ)
release="$remote_root/releases/$stamp"
previous=$(readlink -f "$remote_root/current")
backup="$backup_dir/predeploy-$stamp.db"
switched=false
finished=false
previous_safe=false
failure() {
  code=$?
  trap - EXIT
  if [[ "$finished" != true ]]; then
    if [[ "$switched" == true ]]; then
      # A security marker is only written after the entire release has passed.
      if [[ "$previous_safe" == true ]] && python3 "$release/src/scripts/verify_release_binary.py" "$go_binary" "$previous" --require-security-baseline >/dev/null 2>&1; then
        if ! ln -sfn "$previous" "$remote_root/current.rollback" ||
           ! mv -Tf "$remote_root/current.rollback" "$remote_root/current" ||
           ! systemctl restart kxone-server kxone-worker; then
          systemctl stop kxone-server kxone-worker || true
        fi
      else
        systemctl stop kxone-server kxone-worker || true
        echo 'No verified secure rollback exists; authentication services stopped.' >&2
      fi
    fi
    echo "deployment_failed=$code current_release=$(readlink -f "$remote_root/current") database_backup=$backup" >&2
    systemctl is-active kxone-server kxone-worker >&2 || true
  fi
  exit "$code"
}
trap failure EXIT
[[ "$revision" =~ ^[0-9a-f]{40}-[0-9a-f]{64}$ ]]
[[ "$(sha256sum "$source_archive" | awk '{print $1}')" == "${revision#*-}" ]]
[[ -x "$go_binary" && -r "$env_file" && -f "$database" ]]
[[ "$($go_binary version | awk '{print $3}')" == go1.26.10 ]]
set -a
source "$env_file"
set +a
[[ "$SERVER_ENV" == prod && "$DB_NAME" == "$database" ]]
install -d -o kxone -g kxone -m 0750 "$backup_dir"
install -d -o kxone -g kxone -m 0755 "$release/src" "$release/web" "$release/bin" "$release/evidence"
sha256sum "$source_archive" "$web_archive" > "$release/evidence/uploaded-archives.sha256"
tar -C "$release/src" -xzf "$source_archive"
cp -a "$previous/web/." "$release/web/"
tar -C "$release/web" -xzf "$web_archive"
cd "$release/src"
# The first sid release cannot roll back to the old stateless authenticator.
# Continue with fail-closed recovery when no compatible predecessor exists.
if python3 scripts/verify_release_binary.py "$go_binary" "$previous" --require-security-baseline > "$release/evidence/rollback-binaries.json"; then
  previous_safe=true
else
  previous_safe=false
  echo 'No compatible sid rollback; a failure after migration will stop CRM services.' > "$release/evidence/rollback-policy.txt"
fi
python3 scripts/release_state.py "$database" migrations > "$release/evidence/before.json"
# SQLite online backup, then restore it into a separate migration rehearsal DB.
python3 - "$database" "$backup" "$release/evidence/rehearsal.db" <<'PY'
import sqlite3, sys
source = sqlite3.connect('file:' + sys.argv[1] + '?mode=ro', uri=True)
for destination in sys.argv[2:]:
    with sqlite3.connect(destination) as target:
        source.backup(target)
        assert target.execute('PRAGMA integrity_check').fetchone()[0] == 'ok'
source.close()
PY
# This two-core host also serves other applications. Compile in a low-priority
# resource-capped unit while the current CRM keeps running; no secret env is
# passed to the compiler. A build failure occurs before the switch boundary.
build_binary() {
  local name=$1 package=$2
  systemd-run --quiet --wait --pipe --collect \
    --unit="kxone-build-${stamp}-${name}" --working-directory="$release/src" \
    --property=CPUQuota=25% --property=MemoryMax=512M \
    --property=Nice=19 --property=IOWeight=10 \
    /usr/bin/env GOMAXPROCS=1 GOMEMLIMIT=384MiB CGO_ENABLED=1 GOTOOLCHAIN=local \
    GOPATH="$("$go_binary" env GOPATH)" GOMODCACHE="$("$go_binary" env GOMODCACHE)" \
    GOCACHE="$("$go_binary" env GOCACHE)" \
    "$go_binary" build -p 1 -trimpath -o "$release/bin/$name" "$package"
}
build_binary kxone-server ./cmd/server
build_binary kxone-worker ./cmd/worker
build_binary kxone-migrate ./cmd/tools/migrate
build_binary repair-owner-cycles ./cmd/tools/repair_owner_cycles
build_binary check-encrypted-fields ./cmd/tools/check_encrypted_fields
"$release/bin/check-encrypted-fields" -db "$database" > "$release/evidence/encrypted-fields.json"
python3 scripts/verify_release_binary.py "$go_binary" "$release" > "$release/evidence/candidate-binaries.json"
"$release/bin/kxone-migrate" -db "$release/evidence/rehearsal.db" -migrations migrations > "$release/evidence/migration-rehearsal.log" 2>&1
python3 scripts/release_state.py "$release/evidence/rehearsal.db" migrations > "$release/evidence/rehearsal.json"
python3 - "$release/evidence/rehearsal.json" <<'PY'
import json, sys
assert not json.load(open(sys.argv[1]))['pending']
PY
# Run both secure binaries against the migrated clone and a private Redis process.
if [[ "$previous_safe" == true ]]; then
  python3 scripts/rollback_runtime_drill.py "$release" "$previous" > "$release/evidence/rollback-runtime.json"
else
  python3 scripts/rollback_runtime_drill.py "$release" > "$release/evidence/candidate-runtime.json"
fi
# Exercise the same atomic link replacement used by rollback without touching current.
ln -s "$release" "$release/evidence/rollback-drill"
ln -s "$previous" "$release/evidence/rollback-next"
mv -Tf "$release/evidence/rollback-next" "$release/evidence/rollback-drill"
[[ "$(readlink -f "$release/evidence/rollback-drill")" == "$previous" ]]
[[ -x "$release/evidence/rollback-drill/bin/kxone-server" ]]
rm "$release/evidence/rollback-drill"
printf '%s\n' "$revision" > "$release/REVISION"
(cd "$release/bin" && sha256sum kxone-* repair-owner-cycles) > "$release/evidence/binaries.sha256"
(cd "$release/web" && find . -type f -print0 | sort -z | xargs -0 sha256sum) > "$release/evidence/web.sha256"
chown -R kxone:kxone "$release"
chmod 0640 "$backup" "$release/evidence/rehearsal.db"
# Migrations are additive. A failure never restores the whole live database.
# Mark this boundary before migration: a failed security migration must fail closed.
switched=true
systemctl stop kxone-server kxone-worker
# Recheck the final database after the last writer has stopped.
"$release/bin/check-encrypted-fields" -db "$database" > "$release/evidence/encrypted-fields-final.json"
"$release/bin/kxone-migrate" -db "$database" -migrations migrations > "$release/evidence/migrations.log" 2>&1
ln -sfn "$release" "$remote_root/current.next"
mv -Tf "$remote_root/current.next" "$remote_root/current"
systemctl restart kxone-server kxone-worker
healthy=false
for attempt in {1..15}; do
  if curl -fsS --max-time 3 http://127.0.0.1:18081/health > "$release/evidence/health.json"; then healthy=true; break; fi
  sleep 1
done
[[ "$healthy" == true ]]
systemctl is-active --quiet kxone-server kxone-worker
python3 scripts/release_state.py "$database" migrations --before "$release/evidence/before.json" > "$release/evidence/after.json"
# Verify the running executables, not only the current symlink.
for unit in server worker; do
  pid=$(systemctl show -p MainPID --value "kxone-$unit")
  [[ "$(readlink -f "/proc/$pid/exe")" == "$release/bin/kxone-$unit" ]]
done
curl -fsS --max-time 15 "${public_url%/}/health" > "$release/evidence/public-health.json"
curl -fsS --max-time 15 "${public_url%/}/" > "$release/evidence/public-index.html"
cmp "$release/web/index.html" "$release/evidence/public-index.html"
python3 - "$release" "$public_url" <<'PY'
import hashlib, re, sys, urllib.request
from pathlib import Path
root=Path(sys.argv[1]); url=sys.argv[2].rstrip('/')
for path in set(re.findall(r'(?:src|href)="(/assets/[^\"]+)"', (root/'web/index.html').read_text())):
    with urllib.request.urlopen(url+path, timeout=15) as response: content=response.read()
    assert hashlib.sha256(content).digest() == hashlib.sha256((root/'web'/path.lstrip('/')).read_bytes()).digest(), path
PY
printf 'immutable account identity and session versioning verified\n' > "$release/SECURITY_BASELINE"
printf 'revocable-sid-v1\n' > "$release/SESSION_SECURITY_BASELINE"
if [[ ! -e "$remote_root/security-baseline" ]]; then ln -s "$release" "$remote_root/security-baseline"; fi
finished=true
rm -f "$source_archive" "$web_archive"
echo "immutable_release=$release"
echo "previous_release=$previous"
echo "database_backup=$backup"
cat "$release/evidence/after.json"
