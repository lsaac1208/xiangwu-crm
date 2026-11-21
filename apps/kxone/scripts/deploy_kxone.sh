#!/usr/bin/env bash
set -euo pipefail

# macOS archive metadata becomes unwanted ._ files when unpacked on Linux.
export COPYFILE_DISABLE=1

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
: "${KXONE_SERVER_HOST:?KXONE_SERVER_HOST is required}"
: "${KXONE_SERVER_USER:?KXONE_SERVER_USER is required}"
: "${KXONE_PUBLIC_URL:?KXONE_PUBLIC_URL is required}"

SSH_KEY="${KXONE_SSH_KEY:-}"
SSH_TARGET="${KXONE_SERVER_USER}@${KXONE_SERVER_HOST}"
REMOTE_ROOT="${KXONE_REMOTE_DIR:-/opt/kxone}"
REMOTE_DB="${KXONE_REMOTE_DB:-/var/lib/kxone/kxone.db}"
REMOTE_ENV="${KXONE_REMOTE_ENV:-/etc/kxone/kxone.env}"
REMOTE_BACKUP_DIR="${KXONE_REMOTE_BACKUP_DIR:-/var/backups/kxone}"
REMOTE_GO="${KXONE_REMOTE_GO_BINARY:-/opt/toolchains/go1.26.10/bin/go}"
PUBLIC_HEALTH_URL="${KXONE_PUBLIC_HEALTH_URL:-${KXONE_PUBLIC_URL%/}/health}"
SSH_ARGS=(-o BatchMode=yes -o ConnectTimeout=15 -o ServerAliveInterval=10 -o ServerAliveCountMax=3)
if [[ -n "$SSH_KEY" ]]; then
  SSH_ARGS+=(-i "$SSH_KEY")
fi

BUNDLE_DIR="$(mktemp -d /tmp/kxone-release.XXXXXX)"
SRC_TGZ="$BUNDLE_DIR/$(basename "$BUNDLE_DIR")-src.tgz"
WEB_TGZ="$BUNDLE_DIR/$(basename "$BUNDLE_DIR")-web.tgz"
cleanup() { rm -f "$SRC_TGZ" "$WEB_TGZ"; rmdir "$BUNDLE_DIR"; }
trap cleanup EXIT

cd "$ROOT_DIR"
# Always validate and rebuild the artifacts that this invocation packages.
# A recorded Git commit cannot attest to the ignored contents of web/dist.
GOTOOLCHAIN=go1.26.10 go test ./... -count=1
GOTOOLCHAIN=go1.26.10 go vet ./...
python3 scripts/test_frontend_release_baseline.py
npm ci --prefix web
npm run lint --prefix web
npm run lint:tokens --prefix web
npm test --prefix web -- --run
npm run build --prefix web
npm run check:bundle --prefix web
(cd web && GOTOOLCHAIN=go1.26.10 npx playwright test --config playwright.audit.config.js && GOTOOLCHAIN=go1.26.10 npx playwright test --config playwright.regression.config.js && npx playwright test --config playwright.strengthen.config.js)

tar --exclude='.env' --exclude='bin' --exclude='data' --exclude='._*' \
  --exclude='*.test' \
  --exclude='web/node_modules' --exclude='web/dist' \
  --exclude='web/audit-evidence' --exclude='web/playwright-report' --exclude='web/test-results' \
  --exclude='web/audit-report' --exclude='web/audit-results' --exclude='web/regression-results' \
  --exclude='cms-web/node_modules' --exclude='cms-web/dist' \
  -czf "$SRC_TGZ" .
tar --exclude='._*' -C web/dist -czf "$WEB_TGZ" .

# Check the actual archives before upload, including files already named ._*.
python3 - "$SRC_TGZ" "$WEB_TGZ" <<'PY'
import sys
import tarfile

for path in sys.argv[1:]:
    with tarfile.open(path) as archive:
        for member in archive:
            if any(part.startswith('._') for part in member.name.split('/')):
                raise SystemExit(f'archive contains macOS metadata: {member.name}')
PY

if [[ "${KXONE_DEPLOY_VALIDATE_ONLY:-false}" == "true" ]]; then
  tar -tzf "$SRC_TGZ" >/dev/null
  tar -tzf "$WEB_TGZ" >/dev/null
  echo "immutable CRM deployment inputs validated"
  exit 0
fi

remote_src="/tmp/$(basename "$SRC_TGZ")"
remote_web="/tmp/$(basename "$WEB_TGZ")"
scp "${SSH_ARGS[@]}" "$SRC_TGZ" "$WEB_TGZ" "$SSH_TARGET:/tmp/"

# Copy the remote transaction separately so shell interpolation cannot change it.
remote_script="/tmp/kxone-deploy-$(basename "$SRC_TGZ").sh"
scp "${SSH_ARGS[@]}" "$ROOT_DIR/scripts/deploy_remote.sh" "$SSH_TARGET:$remote_script"
# The archive includes new files that git diff HEAD omits. Identify the exact
# uploaded source, so an uncommitted repair remains traceable without a commit.
revision="$(git rev-parse HEAD)-$(shasum -a 256 "$SRC_TGZ" | awk '{print $1}')"
printf -v remote_command '%q ' bash "$remote_script" "$REMOTE_ROOT" "$REMOTE_DB" "$REMOTE_ENV" "$REMOTE_BACKUP_DIR" "$REMOTE_GO" "$remote_src" "$remote_web" "$KXONE_PUBLIC_URL" "$revision"
ssh "${SSH_ARGS[@]}" "$SSH_TARGET" "$remote_command"
echo "CRM deployment completed: $PUBLIC_HEALTH_URL"
