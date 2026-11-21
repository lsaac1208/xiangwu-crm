#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
: "${KXONE_SERVER_HOST:?KXONE_SERVER_HOST is required}"
: "${KXONE_SERVER_USER:?KXONE_SERVER_USER is required}"

SSH_KEY="${KXONE_SSH_KEY:-}"
SSH_TARGET="${KXONE_SERVER_USER}@${KXONE_SERVER_HOST}"
REMOTE_CURRENT="${KXONE_REMOTE_CURRENT:-/opt/kxone/current}"
REMOTE_DB="${KXONE_REMOTE_DB:-/var/lib/kxone/kxone.db}"
REMOTE_ENV="${KXONE_REMOTE_ENV:-/etc/kxone/kxone.env}"
EXPECTED_SERVER_PORT="${KXONE_EXPECTED_SERVER_PORT:-18081}"
EXPECTED_REDIS_DB="${KXONE_EXPECTED_REDIS_DB:-2}"
EXPECTED_GO_VERSION="${KXONE_EXPECTED_GO_VERSION:-go1.26.10}"
GO_BINARY="${KXONE_REMOTE_GO_BINARY:-/opt/toolchains/go1.26.10/bin/go}"
SSH_ARGS=(-o BatchMode=yes -o ConnectTimeout=10)
if [[ -n "$SSH_KEY" ]]; then
  SSH_ARGS+=(-i "$SSH_KEY")
fi

remote_output="$(ssh "${SSH_ARGS[@]}" "$SSH_TARGET" \
  "REMOTE_CURRENT='$REMOTE_CURRENT' REMOTE_DB='$REMOTE_DB' REMOTE_ENV='$REMOTE_ENV' GO_BINARY='$GO_BINARY' python3 -" <<'PY'
import json
import os
import sqlite3
import subprocess
from pathlib import Path

current = Path(os.environ["REMOTE_CURRENT"])
db_path = Path(os.environ["REMOTE_DB"])
env_path = Path(os.environ["REMOTE_ENV"])
go_binary = Path(os.environ["GO_BINARY"])

safe_keys = {
    "SERVER_HOST", "SERVER_PORT", "SERVER_ENV", "DB_DRIVER", "DB_NAME",
    "REDIS_HOST", "REDIS_PORT", "REDIS_DB", "SERVER_PUBLIC_BASE_URL",
    "MIGRATIONS_DIR", "CMS_ENABLED",
}
safe_env = {}
for raw in env_path.read_text().splitlines():
    if not raw or raw.lstrip().startswith("#") or "=" not in raw:
        continue
    key, value = raw.split("=", 1)
    if key in safe_keys:
        safe_env[key] = value.strip().strip('"').strip("'")

connection = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
try:
    integrity = connection.execute("PRAGMA integrity_check").fetchone()[0]
    foreign_key_errors = connection.execute("PRAGMA foreign_key_check").fetchall()
    migrations = dict(connection.execute("SELECT version, checksum FROM schema_migrations"))
    lead_count, lead_max_id = connection.execute("SELECT COUNT(*), COALESCE(MAX(id), 0) FROM leads").fetchone()
    settings = dict(connection.execute(
        "SELECT setting_key, setting_value FROM system_settings WHERE setting_key IN (?, ?)",
        ("notification.group_robot_followup_enabled", "pool.auto_return_enabled"),
    ))
finally:
    connection.close()

def active(unit):
    return subprocess.run(["systemctl", "is-active", "--quiet", unit]).returncode == 0

go_version = "missing"
if go_binary.is_file():
    go_version = subprocess.check_output([str(go_binary), "version"], text=True).split()[2]

print(json.dumps({
    "env": safe_env,
    "database": {
        "path": str(db_path),
        "inode": db_path.stat().st_ino,
        "integrity": integrity,
        "foreign_key_errors": len(foreign_key_errors),
        "lead_count": lead_count,
        "lead_max_id": lead_max_id,
    },
    "migrations": migrations,
    "settings": settings,
    "services": {unit: active(unit) for unit in ["kxone-server", "kxone-worker", "nginx", "redis-server"]},
    "current_release": str(current.resolve()),
    "release_layout": {
        "server": (current / "bin/kxone-server").is_file(),
        "worker": (current / "bin/kxone-worker").is_file(),
        "source": (current / "src/go.mod").is_file(),
        "web": (current / "web/index.html").is_file(),
    },
    "go_version": go_version,
}, ensure_ascii=False))
PY
)"

python3 - "$remote_output" "$EXPECTED_SERVER_PORT" "$EXPECTED_REDIS_DB" "$EXPECTED_GO_VERSION" "$ROOT_DIR" <<'PY'
import hashlib
import json
import sys
from pathlib import Path

state = json.loads(sys.argv[1])
expected_port, expected_redis_db, expected_go = sys.argv[2:5]
root = Path(sys.argv[5])
errors = []

database = state["database"]
if database["integrity"] != "ok":
    errors.append(f"database integrity is {database['integrity']!r}")
if database["foreign_key_errors"]:
    errors.append(f"database has {database['foreign_key_errors']} foreign key errors")
for unit, is_active in state["services"].items():
    if not is_active:
        errors.append(f"{unit} is not active")
if state["env"].get("SERVER_PORT") != expected_port:
    errors.append(f"SERVER_PORT={state['env'].get('SERVER_PORT')}, expected {expected_port}")
if state["env"].get("REDIS_DB") != expected_redis_db:
    errors.append(f"REDIS_DB={state['env'].get('REDIS_DB')}, expected {expected_redis_db}")
if state["env"].get("DB_NAME") != database["path"]:
    errors.append(f"DB_NAME={state['env'].get('DB_NAME')}, expected {database['path']}")
if state["go_version"] != expected_go:
    errors.append(f"Go={state['go_version']}, expected {expected_go}")
for item, present in state["release_layout"].items():
    if not present:
        errors.append(f"current release is missing {item}")
if state["settings"].get("notification.group_robot_followup_enabled", "false").lower() == "true":
    errors.append("follow-up robot notification must remain disabled")

remote_migrations = state["migrations"]
local_migrations = {}
for path in sorted((root / "migrations").glob("*.sql")):
    version = path.name.split("_", 1)[0]
    if version in local_migrations:
        errors.append(f"duplicate migration {version}")
    local_migrations[version] = hashlib.sha256(path.read_bytes()).hexdigest()
for version, checksum in remote_migrations.items():
    if local_migrations.get(version) != checksum:
        errors.append(f"applied migration {version} checksum mismatch or missing locally")
pending = sorted(set(local_migrations) - set(remote_migrations))
if pending and remote_migrations and pending[0] < max(remote_migrations):
    errors.append("pending migration is older than an already applied migration")
state["pending_migrations"] = pending
state["applied_migration_count"] = len(remote_migrations)

print(json.dumps(state, ensure_ascii=False, indent=2))
if errors:
    print("preflight errors:", file=sys.stderr)
    for error in errors:
        print(f"- {error}", file=sys.stderr)
    raise SystemExit(1)
print("production_read_only_preflight=pass")
PY
