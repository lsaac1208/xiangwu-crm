#!/usr/bin/env python3
"""Exercise new -> previous secure server on a cloned DB, isolated loopback Redis."""
import json
import errno
import os
from pathlib import Path
import re
import secrets
import shutil
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
import uuid


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


release = Path(sys.argv[1])
previous = Path(sys.argv[2]) if len(sys.argv) > 2 else None
if previous is not None:
    assert (previous / 'SESSION_SECURITY_BASELINE').read_text().strip() == 'revocable-sid-v1'
candidates = (release, previous) if previous is not None else (release,)

# A cloned database can still contain absolute paths to real files. Run the
# complete drill (Redis, servers and HTTP probes) in a private network and a
# read-only filesystem, allowing writes only to a disposable clone directory.
if os.environ.get('KXONE_RUNTIME_DRILL_SANDBOXED') != '1':
    # /run is visible inside PrivateTmp; /tmp and /var/tmp are replaced there.
    with tempfile.TemporaryDirectory(prefix='kxone-runtime-drill-', dir='/run') as temporary:
        sandbox = Path(temporary)
        with sqlite3.connect('file:' + str(release / 'evidence/rehearsal.db') + '?mode=ro', uri=True) as original:
            with sqlite3.connect(sandbox / 'rehearsal.db') as clone:
                original.backup(clone)
        config_keys = {'PATH', 'LANG', 'LC_ALL', 'TZ', 'TRUSTED_PROXIES',
                       'FIELD_ENCRYPTION_KEY', 'PII_ENCRYPTION_KEY', 'MOBILE_HASH_KEY',
                       'CORS_ALLOWED_ORIGINS', 'PUBLIC_FORM_ORIGINS'}
        config_prefixes = ('SERVER_', 'DB_', 'REDIS_', 'JWT_', 'CRM_', 'AD_LEAD_', 'CMS_', 'AGENT_', 'KXONE_')
        environment = {key: value for key, value in os.environ.items()
                       if re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*', key)
                       and (key in config_keys or key.startswith(config_prefixes))}
        canary = release / 'evidence' / ('runtime-isolation-canary-' + uuid.uuid4().hex)
        canary.write_text('unchanged')
        canary.chmod(0o600)
        environment.update(KXONE_RUNTIME_DRILL_SANDBOXED='1', KXONE_RUNTIME_DRILL_DIRECTORY=str(sandbox), KXONE_RUNTIME_DRILL_CANARY=str(canary), PYTHONDONTWRITEBYTECODE='1')
        # EnvironmentFile keeps credentials out of argv and transient-unit
        # Environment properties. Its parent and contents are root-private.
        env_file = sandbox / 'environment'
        env_file.write_text(''.join(key + '=\"' + value.replace('\\', '\\\\').replace('\"', '\\\"') + '\"\n'
                                    for key, value in environment.items()))
        env_file.chmod(0o600)
        unit = 'kxone-runtime-drill-' + uuid.uuid4().hex
        command = ['systemd-run', '--quiet', '--wait', '--pipe', '--collect', '--unit=' + unit]
        for key, value in {'EnvironmentFile': str(env_file), 'ProtectSystem': 'strict',
                           'ReadWritePaths': str(sandbox), 'ProtectHome': 'true',
                           'PrivateTmp': 'true', 'PrivateNetwork': 'true',
                           'NoNewPrivileges': 'true', 'CPUQuota': '25%',
                           'MemoryMax': '512M', 'Nice': '19', 'IOWeight': '10',
                           'TimeoutStopSec': '5s', 'RuntimeMaxSec': '90s'}.items():
            command.append('--property=' + key + '=' + value)
        command += [sys.executable, str(Path(__file__).resolve()), str(release)]
        if previous is not None:
            command.append(str(previous))
        try:
            checked = subprocess.run(command, text=True, capture_output=True, timeout=110)
            if checked.returncode:
                raise RuntimeError('isolated runtime unit failed; inspect protected runtime logs')
            proof = json.loads(checked.stdout)
            proof['isolation'] = {'private_network': True, 'read_only_filesystem': True, 'private_tmp': True}
            print(json.dumps(proof))
        finally:
            # Stop the whole service cgroup before removing its writable clone.
            try:
                subprocess.run(['systemctl', 'stop', unit], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15)
            except subprocess.TimeoutExpired:
                subprocess.run(['systemctl', 'kill', '--kill-whom=all', '--signal=SIGKILL', unit], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
            for log in sandbox.glob('runtime-*.log'):
                target = release / 'evidence' / log.name
                shutil.copyfile(log, target)
                target.chmod(0o600)
            canary.unlink()
    sys.exit(0)

evidence = Path(os.environ['KXONE_RUNTIME_DRILL_DIRECTORY'])
interfaces = [name for _, name in socket.if_nameindex()]
if any(name != 'lo' for name in interfaces):
    raise RuntimeError('runtime drill must have only loopback networking')
try:
    Path(os.environ['KXONE_RUNTIME_DRILL_CANARY']).write_text('unexpected writable filesystem')
except OSError as error:
    if error.errno not in (errno.EROFS, errno.EACCES, errno.EPERM):
        raise
else:
    raise RuntimeError('runtime drill filesystem isolation is ineffective')
redis_port = free_port()
redis = subprocess.Popen(['redis-server', '--bind', '127.0.0.1', '--port', str(redis_port), '--save', '', '--appendonly', 'no'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
results = []
try:
    for candidate in candidates:
        port = free_port()
        env = dict(os.environ, SERVER_HOST='127.0.0.1', SERVER_PORT=str(port), DB_NAME=str(evidence / 'rehearsal.db'), MIGRATIONS_DIR=str(candidate / 'src/migrations'), REDIS_HOST='127.0.0.1', REDIS_PORT=str(redis_port), REDIS_PASSWORD='', REDIS_DB='0', JWT_SECRET=secrets.token_hex(32), CRM_BOOTSTRAP_USERNAME='', CRM_BOOTSTRAP_PASSWORD='', CMS_ENABLED='false', AGENT_STORAGE_DIR=str(evidence / ('agent-runtime-' + candidate.name)))
        with (evidence / ('runtime-' + candidate.name + '.log')).open('w') as log:
            process = subprocess.Popen([str(candidate / 'bin/kxone-server')], env=env, cwd=candidate / 'src', stdout=log, stderr=log)
            try:
                for attempt in range(50):
                    if process.poll() is not None:
                        raise RuntimeError('isolated server exited: ' + str(candidate))
                    try:
                        with urllib.request.urlopen(f'http://127.0.0.1:{port}/health', timeout=1) as response:
                            assert response.status == 200
                        break
                    except (urllib.error.URLError, TimeoutError):
                        time.sleep(.2)
                else:
                    raise RuntimeError('isolated health timeout')
                try:
                    urllib.request.urlopen(f'http://127.0.0.1:{port}/api/leads', timeout=2)
                    raise RuntimeError('unauthenticated access unexpectedly allowed')
                except urllib.error.HTTPError as error:
                    assert error.code == 401, error.code
                results.append({'release': str(candidate), 'health': True, 'unauthenticated_status': 401})
            finally:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
finally:
    redis.terminate()
    redis.wait(timeout=10)
print(json.dumps({'isolated_database': str(evidence / 'rehearsal.db'), 'new_then_rollback': results, 'rollback_available': previous is not None, 'sandbox_checks': {'read_only_canary': True, 'network_interfaces': interfaces}}))
