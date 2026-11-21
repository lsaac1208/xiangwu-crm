#!/usr/bin/env python3
"""Read-only release evidence; normal business inserts/updates are observations."""
import argparse
import hashlib
import json
import sqlite3
from pathlib import Path

FLAGS = ('appointment.enabled', 'workflow.v2_enabled', 'pool.enabled', 'pool.auto_return_enabled', 'notification.group_robot_followup_enabled')

def snapshot(database, migrations):
    path = Path(database)
    connection = sqlite3.connect(f'file:{path}?mode=ro', uri=True)
    try:
        applied = dict(connection.execute('SELECT version, checksum FROM schema_migrations'))
        files = sorted(Path(migrations).glob('*.sql'))
        local = {f.name.split('_', 1)[0]: hashlib.sha256(f.read_bytes()).hexdigest() for f in files}
        if len(local) != len(files):
            raise ValueError('duplicate migration version')
        listed = {}
        for line in (Path(migrations) / 'SHA256SUMS').read_text().splitlines():
            checksum, name = line.split()
            listed[name] = checksum
        if listed != {f.name: local[f.name.split('_', 1)[0]] for f in files}:
            raise ValueError('migration checksum manifest mismatch')
        for version, checksum in applied.items():
            if local.get(version) != checksum:
                raise ValueError(f'applied migration mismatch: {version}')
        pending = sorted(set(local) - set(applied))
        if pending and applied and pending[0] < max(applied):
            raise ValueError('out-of-order pending migration')
        integrity = connection.execute('PRAGMA integrity_check').fetchone()[0]
        foreign_keys = len(connection.execute('PRAGMA foreign_key_check').fetchall())
        if integrity != 'ok' or foreign_keys:
            raise ValueError('database integrity/foreign key check failed')
        count, maximum = connection.execute('SELECT COUNT(*), COALESCE(MAX(id),0) FROM leads').fetchone()
        flags = dict(connection.execute('SELECT setting_key, setting_value FROM system_settings WHERE setting_key IN (' + ','.join('?' for _ in FLAGS) + ')', FLAGS))
        agent_enabled = None
        if connection.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name='ai_settings'").fetchone():
            row = connection.execute('SELECT enabled FROM ai_settings WHERE id=1').fetchone()
            if row:
                if row[0] not in (0, 1):
                    raise ValueError('invalid AI feature switch')
                agent_enabled = bool(row[0])
        return dict(inode=path.stat().st_ino, integrity=integrity, foreign_key_errors=foreign_keys,
                    lead_count=count, lead_max_id=maximum, flags=flags, agent_enabled=agent_enabled, applied=applied, pending=pending)
    finally:
        connection.close()

def verify(before, after):
    if before['inode'] != after['inode']:
        raise ValueError('live database file was replaced')
    if before['flags'] != after['flags']:
        raise ValueError('feature switches changed during deployment')
    # Initial additive Agent structure may be absent or initialized disabled.
    # Enabling analysis is an explicit, separately accepted rollout action.
    old_agent, new_agent = before.get('agent_enabled'), after.get('agent_enabled')
    if old_agent != new_agent and not (old_agent is None and new_agent is False):
        raise ValueError('AI feature switch changed during deployment')
    if after['pending']:
        raise ValueError('release migrations remain pending')
    # Lead counts and versions may change while real staff are using CRM.
    return {'lead_count_delta': after['lead_count'] - before['lead_count'], 'lead_max_id_delta': after['lead_max_id'] - before['lead_max_id']}

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('database')
    parser.add_argument('migrations')
    parser.add_argument('--before')
    args = parser.parse_args()
    current = snapshot(args.database, args.migrations)
    if args.before:
        current['business_observation'] = verify(json.loads(Path(args.before).read_text()), current)
    print(json.dumps(current, ensure_ascii=False, indent=2))
