#!/usr/bin/env bash
# Frontend-only release: preserve running services, schema and opened-page assets.
set -Eeuo pipefail
[[ $# == 5 || $# == 6 ]]
remote_root=$(readlink -f "$1")
web_archive=$2
source_archive=$3
public_url=${4%/}
revision=$5
go_binary=${6:-${KXONE_REMOTE_GO_BINARY:-/opt/toolchains/go1.26.10/bin/go}}
# Share the full deployment lock: both workflows replace the same current link.
exec 9>"$remote_root/deploy.lock"
flock -n 9
[[ -L "$remote_root/current" && -f "$web_archive" && -f "$source_archive" ]]
previous=$(readlink -f "$remote_root/current")
[[ "$previous" == "$remote_root/releases/"* ]]
[[ -d "$previous/web" && -d "$previous/src" && -x "$previous/bin/kxone-server" && -x "$previous/bin/kxone-worker" ]]
# Refuse an unverified predecessor before staging or switching any release.
python3 "$previous/src/scripts/verify_release_binary.py" "$go_binary" "$previous" --require-security-baseline >/dev/null
staging=''
release=''
rollback_release=''
finish() {
  code=$?
  trap - EXIT HUP INT TERM
  set +e
  # Inspect the real link, including an interruption immediately after mv.
  if [[ "$code" != 0 && -n "$release" && "$(readlink -f "$remote_root/current")" == "$release" ]]; then
    if [[ -L "$staging/current-rollback" ]] && mv -Tf "$staging/current-rollback" "$remote_root/current"; then
      echo "frontend_rollback=$rollback_release" >&2
      if ! curl -fsS --max-time 10 "$public_url/" | cmp "$rollback_release/web/index.html" -; then
        echo 'frontend_rollback_public_check_failed=true' >&2
      fi
    else
      echo "frontend_rollback_failed=true current=$(readlink -f "$remote_root/current")" >&2
    fi
  fi
  if [[ "$code" != 0 ]]; then echo "frontend_deploy_failed=$code" >&2; fi
  if [[ -n "$staging" && "$staging" == "$remote_root/releases/.ui-stage."* ]]; then rm -rf -- "$staging"; fi
  exit "$code"
}
trap finish EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
server_pid=$(systemctl show kxone-server -p MainPID --value)
worker_pid=$(systemctl show kxone-worker -p MainPID --value)
[[ "$server_pid" != 0 && "$worker_pid" != 0 ]]
curl -fsS --max-time 5 http://127.0.0.1:18081/health >/dev/null
staging=$(mktemp -d "$remote_root/releases/.ui-stage.XXXXXX")
# Validate archives before copying any previous files: an empty or nested dist
# archive must never pass by accidentally reusing the previous index.html.
python3 - "$web_archive" "$source_archive" "$staging" <<'PY'
import hashlib, pathlib, re, shutil, sys, tarfile
stage = pathlib.Path(sys.argv[3])
def unpack(archive, destination, source=False):
    destination.mkdir()
    with tarfile.open(archive, 'r:gz') as bundle:
        entries = []
        seen = set()
        for member in bundle.getmembers():
            path = pathlib.PurePosixPath(member.name)
            if path.is_absolute() or '..' in path.parts or '\\' in member.name:
                raise ValueError('unsafe archive path: ' + member.name)
            if str(path) == '.':
                if not member.isdir(): raise ValueError('invalid archive root')
                continue
            if not (member.isfile() or member.isdir()):
                raise ValueError('archive links/special files are not allowed: ' + member.name)
            if source and path.parts[0] not in {'web', 'scripts'}:
                raise ValueError('frontend source must contain only web/ and scripts/: ' + member.name)
            if path in seen:
                raise ValueError('duplicate archive member: ' + member.name)
            seen.add(path)
            entries.append((member, destination.joinpath(*path.parts)))
        if not entries: raise ValueError('empty archive')
        # All members have been checked before any extraction occurs.
        for member, target in entries:
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                with bundle.extractfile(member) as incoming, target.open('xb') as outgoing:
                    shutil.copyfileobj(incoming, outgoing)
                target.chmod(0o755 if member.mode & 0o111 else 0o644)
web = stage / 'web'
source = stage / 'source'
unpack(sys.argv[1], web)
unpack(sys.argv[2], source, source=True)
assert (source / 'web/package.json').is_file(), 'frontend source package.json missing'
assert (source / 'web/src/main.js').is_file(), 'frontend source entry missing'
html = (web / 'index.html').read_text()
entry_assets = set(re.findall(r'(?:src|href|data-src)="(/assets/[^\"]+)"', html))
assert entry_assets, 'new index has no entry assets'
assert 'type="module"' in html and 'vite-legacy-entry' in html, 'modern/legacy entry missing'
def exists(reference, owner):
    ref = reference.split('?', 1)[0].split('#', 1)[0]
    path = web / ref.lstrip('/') if ref.startswith(('/', 'assets/')) else owner.parent / ref
    assert path.resolve().is_relative_to(web.resolve()), 'resource outside web root: ' + reference
    assert path.is_file(), 'resource missing: ' + reference
for asset in entry_assets: exists(asset, web / 'index.html')
resource_pattern = re.compile(r'''["']((?:/assets/|assets/|\./|\.\./)[^"'\\\s]+?\.(?:js|mjs|css|png|jpe?g|svg|gif|webp|ico|woff2?|ttf)(?:\?[^"'\\\s]*)?)["']''')
for owner in web.rglob('*'):
    if owner.suffix not in {'.js', '.mjs', '.css', '.html'}: continue
    content = owner.read_text()
    for ref in resource_pattern.findall(content): exists(ref, owner)
    if owner.suffix == '.css':
        for ref in re.findall(r'url\(\s*["\']?([^\s)"\']+)', content):
            if not ref.startswith(('data:', 'http:', 'https:', '#', '//')): exists(ref, owner)
(stage / 'new-web.sha256').write_text(''.join(
    hashlib.sha256(path.read_bytes()).hexdigest() + '  ' + str(path.relative_to(web)) + '\n'
    for path in sorted(web.rglob('*')) if path.is_file()
))
PY
stamp=$(date -u +%Y%m%dT%H%M%SZ)-ui
release=$(mktemp -d "$remote_root/releases/$stamp.XXXXXX")
rollback_release="$release-rollback"
install -d -o kxone -g kxone -m 0755 "$release/evidence" "$rollback_release/evidence"
cp -a "$previous/bin" "$previous/src" "$previous/web" "$release/"
cp -a "$previous/bin" "$previous/src" "$previous/web" "$rollback_release/"
# Preserve hashed resources in both directions. Same-name/different-content
# resources would invalidate an already-open page and are rejected before switch.
python3 - "$previous/web" "$staging/web" <<'PY'
import pathlib, sys
previous, incoming = map(pathlib.Path, sys.argv[1:])
for new in (incoming / 'assets').rglob('*'):
    if not new.is_file(): continue
    old = previous / new.relative_to(incoming)
    assert not old.exists() or old.read_bytes() == new.read_bytes(), 'asset name reused with different content: ' + str(new.relative_to(incoming))
PY
cp -a "$staging/web/." "$release/web/"
cp -a "$staging/source/." "$release/src/"
# The rollback uses the old application and source with the resource union.
if [[ -d "$staging/web/assets" ]]; then
  install -d "$rollback_release/web/assets"
  cp -a "$staging/web/assets/." "$rollback_release/web/assets/"
fi
diff -qr "$previous/bin" "$release/bin" >/dev/null
diff -qr "$previous/bin" "$rollback_release/bin" >/dev/null
cmp "$previous/web/index.html" "$rollback_release/web/index.html"
printf '%s\n' "$revision" >"$release/REVISION"
[[ ! -f "$previous/REVISION" ]] || cp -a "$previous/REVISION" "$rollback_release/REVISION"
printf 'frontend_only=true\nprevious=%s\nrollback=%s\nserver_pid=%s\nworker_pid=%s\n' "$previous" "$rollback_release" "$server_pid" "$worker_pid" >"$release/evidence/frontend-release.txt"
printf 'frontend_compatibility_rollback=true\nprevious=%s\nfailed_release=%s\n' "$previous" "$release" >"$rollback_release/evidence/frontend-release.txt"
for destination in "$release" "$rollback_release"; do
  cp -a "$previous/SECURITY_BASELINE" "$previous/SESSION_SECURITY_BASELINE" "$destination/"
  python3 "$destination/src/scripts/verify_release_binary.py" "$go_binary" "$destination" --require-security-baseline >"$destination/evidence/rollback-binaries.json"
  (cd "$destination/web" && find . -type f -print0 | sort -z | xargs -0 sha256sum) >"$destination/evidence/web.sha256"
  (cd "$destination/bin" && sha256sum kxone-server kxone-worker) >"$destination/evidence/binaries.sha256"
  chown -R kxone:kxone "$destination"
  chmod 0755 "$destination"
done
cp "$staging/new-web.sha256" "$release/evidence/new-web.sha256"
[[ "$(readlink -f "$remote_root/current")" == "$previous" ]]
# Prepare both links before the first switch so rollback cannot be blocked by
# stale temporary links or a signal between mv and a shell state assignment.
ln -s "$rollback_release" "$staging/current-rollback"
ln -s "$release" "$staging/current-next"
mv -Tf "$staging/current-next" "$remote_root/current"
curl -fsS --max-time 10 "$public_url/health" >"$release/evidence/health.json"
curl -fsS --max-time 10 "$public_url/" >"$release/evidence/public-index.html"
cmp "$release/web/index.html" "$release/evidence/public-index.html"
for path in /leads /admin/settings /login; do
  curl -fsS --max-time 10 "$public_url$path" | cmp "$release/web/index.html" -
done
python3 - "$staging/web" "$public_url" <<'PY'
import concurrent.futures, hashlib, pathlib, sys, urllib.parse, urllib.request
root = pathlib.Path(sys.argv[1])
def verify(path):
    relative = path.relative_to(root).as_posix()
    with urllib.request.urlopen(sys.argv[2] + '/' + urllib.parse.quote(relative), timeout=10) as response:
        assert hashlib.sha256(response.read()).digest() == hashlib.sha256(path.read_bytes()).digest(), 'published asset mismatch: ' + relative
with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
    list(pool.map(verify, (path for path in root.rglob('*') if path.is_file())))
PY
[[ "$(systemctl show kxone-server -p MainPID --value)" == "$server_pid" ]]
[[ "$(systemctl show kxone-worker -p MainPID --value)" == "$worker_pid" ]]
systemctl is-active --quiet kxone-server kxone-worker
printf 'verified=true\n' >>"$release/evidence/frontend-release.txt"
echo "frontend_release=$release"
echo "previous_release=$previous"
echo "rollback_release=$rollback_release"
echo "services_unchanged=true"
