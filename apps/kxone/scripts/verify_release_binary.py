#!/usr/bin/env python3
"""Verify actual binary dependencies before selecting an Agent rollback target."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess

def validate_build_info(text):
    lines = text.splitlines()
    if not lines or not lines[0].endswith(': go1.26.10'):
        raise ValueError('release binary must use Go 1.26.10')
    driver = None
    cgo = False
    previous_driver = False
    for line in lines[1:]:
        fields = line.split()
        if not fields:
            continue
        if fields[0] == '=>' and previous_driver:
            raise ValueError('unverified replacement SQLite driver')
        if fields[0] == 'build' and re.search(r'\blibsqlite3\b', line):
            raise ValueError('system-linked SQLite is not the verified embedded driver')
        previous_driver = fields[:2] == ['dep', 'github.com/mattn/go-sqlite3']
        if previous_driver:
            if driver is not None or len(fields) < 3:
                raise ValueError('ambiguous SQLite driver metadata')
            driver = fields[2]
        if fields == ['build', 'CGO_ENABLED=1']:
            cgo = True
    if driver != 'v1.14.52' or not cgo:
        raise ValueError('release lacks verified SQLite driver v1.14.52 with CGO')
    return dict(go_version='go1.26.10', sqlite_driver=driver, cgo=True)

def inspect(go_binary, release, require_security=False):
    root = Path(release).resolve(strict=True)
    if require_security and (not (root / 'SECURITY_BASELINE').is_file() or not (root / 'SESSION_SECURITY_BASELINE').is_file() or (root / 'SESSION_SECURITY_BASELINE').read_text().strip() != 'revocable-sid-v1'):
        raise ValueError('release lacks verified authentication baseline')
    binaries = {}
    for name in ('kxone-server', 'kxone-worker'):
        binary = (root / 'bin' / name).resolve(strict=True)
        if binary.parent != root / 'bin':
            raise ValueError('release binary escapes immutable directory')
        metadata = subprocess.check_output([str(go_binary), 'version', '-m', str(binary)], text=True, timeout=15)
        binaries[name] = dict(validate_build_info(metadata), sha256=hashlib.sha256(binary.read_bytes()).hexdigest())
    return dict(release=str(root), authentication_baseline=(root / 'SECURITY_BASELINE').is_file(), binaries=binaries)

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('go_binary')
    parser.add_argument('release')
    parser.add_argument('--require-security-baseline', action='store_true')
    args = parser.parse_args()
    print(json.dumps(inspect(args.go_binary,args.release,args.require_security_baseline),indent=2))
