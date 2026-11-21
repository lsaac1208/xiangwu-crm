#!/usr/bin/env python3
"""Preview or remove unreferenced, expired CRM release directories."""
import argparse
from collections import defaultdict
from datetime import datetime, timezone
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
from zoneinfo import ZoneInfo

import verify_release_binary


def release_time(name):
    utc = re.fullmatch(r'(\d{8}T\d{6}Z)(?:-ui\.[A-Za-z0-9]+(?:-rollback)?)?', name)
    legacy = re.fullmatch(r'(\d{14})(?:-[A-Za-z0-9-]+)?', name)
    try:
        if utc:
            return datetime.strptime(utc[1], '%Y%m%dT%H%M%SZ').replace(tzinfo=timezone.utc)
        if legacy:
            return datetime.strptime(legacy[1], '%Y%m%d%H%M%S').replace(tzinfo=ZoneInfo('Asia/Shanghai'))
    except ValueError:
        pass
    return None


def release_reference(base, target):
    target = Path(str(target).removesuffix(' (deleted)'))
    if not target.is_relative_to(base):
        return None
    target = target.resolve()
    if target.is_relative_to(base) and target != base:
        return base / target.relative_to(base).parts[0]
    return None


def root_references(root, proc_root):
    base = root / 'releases'
    references = set()
    for path in root.iterdir():
        if not path.is_symlink():
            continue
        target = path.resolve(strict=True)
        reference = release_reference(base, target)
        if reference is not None:
            references.add(reference)
    for process in Path(proc_root).iterdir():
        if not process.name.isdecimal():
            continue
        for name in ['exe', 'cwd']:
            try:
                reference = release_reference(base, os.readlink(process / name))
            except FileNotFoundError:
                continue
            if reference is not None:
                references.add(reference)
    return references


def mount_points(proc_root):
    points = []
    for line in (Path(proc_root) / 'self/mountinfo').read_text().splitlines():
        fields = line.split()
        if len(fields) < 7 or '-' not in fields:
            raise ValueError('invalid mount information')
        points.append(Path(re.sub(r'\\([0-7]{3})', lambda match: chr(int(match[1], 8)), fields[4])))
    return points


def measure_directory(path, device, mounts):
    """Do not follow links, cross filesystems or descend into mount points."""
    state = path.lstat()
    if path.is_symlink() or not path.is_dir() or state.st_dev != device or os.path.ismount(path) or any(mount == path or mount.is_relative_to(path) for mount in mounts):
        raise ValueError('unsafe release directory')
    if any((path / name).is_symlink() or not (path / name).is_dir() for name in ['bin', 'src', 'web']):
        raise ValueError('unexpected release structure')
    if not (path / 'web/index.html').is_file() or (path / 'web/index.html').is_symlink():
        raise ValueError('release index missing')
    allocated = 0
    seen = set()
    def unreadable(error):
        raise error
    for parent, directories, files in os.walk(path, followlinks=False, onerror=unreadable):
        for child in [Path(parent), *(Path(parent) / n for n in directories + files)]:
            entry = child.lstat()
            if entry.st_dev != device or (not child.is_symlink() and os.path.ismount(child)):
                raise ValueError('mounted release content')
            key = (entry.st_dev, entry.st_ino)
            if key not in seen:
                seen.add(key)
                allocated += entry.st_blocks * 512
    return {'identity': [state.st_dev, state.st_ino, state.st_mtime_ns], 'bytes': allocated}


def frontend_dependencies(path, base):
    source = path / 'evidence/frontend-release.txt'
    if (path / 'evidence').is_symlink() or source.is_symlink():
        raise ValueError('linked frontend metadata')
    if not source.exists():
        return {}
    if source.stat().st_size > 32768:
        raise ValueError('unexpected frontend metadata')
    fields = dict(line.split('=', 1) for line in source.read_text().splitlines() if '=' in line)
    result = {}
    for key in ['previous', 'rollback', 'failed_release']:
        if key not in fields:
            continue
        target = Path(fields[key]).resolve()
        if target.parent != base:
            raise ValueError('frontend reference outside releases')
        result[key] = target
    return result


def create_plan(root, go_binary, now, proc_root, keep_days, keep_verified):
    base = root / 'releases'
    if base.is_symlink() or not base.is_dir():
        raise ValueError('invalid releases root')
    current = (root / 'current').resolve(strict=True)
    if current.parent != base or current.is_symlink():
        raise ValueError('current outside releases')
    verified = {current: verify_release_binary.inspect(go_binary, current, True)}
    references = root_references(root, proc_root)
    mounts = mount_points(proc_root)
    reasons = defaultdict(set)
    for path in references:
        reasons[path].add('root_or_process_reference')
    records, skipped, metadata = {}, [], {}
    # Live dependencies must be collected even for an unrecognized name.
    for path in references:
        metadata[path] = frontend_dependencies(path, base)
    for path in sorted(base.iterdir()):
        stamp = release_time(path.name)
        if stamp is None or path.is_symlink():
            skipped.append({'name': path.name, 'reason': 'unrecognized_or_link'})
            continue
        try:
            measurement = measure_directory(path, base.stat().st_dev, mounts)
            metadata[path] = frontend_dependencies(path, base)
        except ValueError:
            if path in references:
                raise
            skipped.append({'name': path.name, 'reason': 'unsafe_structure_or_mount'})
            continue
        records[path] = dict(measurement, timestamp=stamp)
        if (now - stamp).total_seconds() < keep_days * 86400 or now.timestamp() - path.stat().st_mtime < keep_days * 86400:
            reasons[path].add('within_keep_days')
    # A compatibility rollback is part of its primary, not a second version.
    eligible = []
    for path in records:
        marker = path / 'SESSION_SECURITY_BASELINE'
        if not marker.is_file():
            continue
        if marker.is_symlink() or marker.read_text().strip() != 'revocable-sid-v1':
            reasons[path].add('unknown_session_baseline')
        elif not path.name.endswith('-rollback'):
            eligible.append(path)
    eligible.sort(key=lambda path: records[path]['timestamp'], reverse=True)
    newest = []
    for path in eligible:
        if path not in verified:
            verified[path] = verify_release_binary.inspect(go_binary, path, True)
        newest.append(path.name)
        reasons[path].add('latest_verified_primary')
        if len(newest) == keep_verified:
            break
    # Protect only direct live dependencies, not the entire historical chain.
    for path in references:
        for target in metadata.get(path, {}).values():
            if not target.is_dir():
                raise ValueError('missing referenced frontend dependency')
            reasons[target].add('live_frontend_dependency')
    pairs = defaultdict(set)
    for path in records:
        companion = path.with_name(path.name + '-rollback')
        if companion in records:
            pairs[path].add(companion)
            pairs[companion].add(path)
        for key in ['rollback', 'failed_release']:
            target = metadata.get(path, {}).get(key)
            if target in records:
                pairs[path].add(target)
                pairs[target].add(path)
    pending = list(reasons)
    while pending:
        path = pending.pop()
        for companion in pairs[path]:
            if not reasons[companion]:
                reasons[companion].add('frontend_companion')
                pending.append(companion)
    protected, candidates = [], []
    for path, record in records.items():
        row = {'name': path.name, 'bytes': record['bytes'], 'identity': record['identity']}
        if reasons[path]:
            protected.append(dict(row, reasons=sorted(reasons[path])))
        else:
            candidates.append(row)
    signature = {'current': current.name, 'references': sorted(str(p) for p in references), 'protected': protected, 'candidates': candidates, 'skipped': skipped}
    return dict(signature, verified_primaries=newest, fingerprint=hashlib.sha256(json.dumps(signature, sort_keys=True).encode()).hexdigest())


def run_cleanup(root, *, go_binary, now=None, proc_root=Path('/proc'), apply=False, keep_days=7, keep_verified=5, expected_plan=None):
    root = Path(root).resolve(strict=True)
    base = root / 'releases'
    now = now or datetime.now(timezone.utc)
    report = {'mode': 'apply' if apply else 'dry-run', 'result': 'ok', 'policy': {'keep_days': keep_days, 'keep_verified': keep_verified}, 'protected': [], 'candidates': [], 'deleted': [], 'skipped': []}
    if keep_days < 1 or keep_verified < 1:
        raise ValueError('retention values must be positive')
    if (root / 'deploy.lock').is_symlink():
        raise ValueError('deployment lock must not be a symlink')
    with (root / 'deploy.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            report['result'] = 'skipped_lock'
            return report
        try:
            report['disk_free_before'] = shutil.disk_usage(root).free
            plan = create_plan(root, go_binary, now, proc_root, keep_days, keep_verified)
            report.update(plan)
            if expected_plan is not None and (expected_plan['fingerprint'] != plan['fingerprint'] or expected_plan['policy'] != report['policy']):
                report['result'] = 'stale_plan'
                return report
            if apply and not shutil.rmtree.avoids_symlink_attacks:
                raise RuntimeError('safe directory deletion unavailable')
            for row in report['candidates']:
                if not apply:
                    break
                references = sorted(str(p) for p in root_references(root, proc_root))
                if references != plan['references'] or (root / 'current').resolve().name != plan['current']:
                    raise RuntimeError('release references changed')
                path = base / row['name']
                fresh = measure_directory(path, base.stat().st_dev, mount_points(proc_root))
                if fresh['identity'] != row['identity'] or fresh['bytes'] != row['bytes']:
                    raise RuntimeError('candidate directory changed')
                shutil.rmtree(path)
                report['deleted'].append(row)
            report['disk_free_after'] = shutil.disk_usage(root).free
            report['deleted_allocated_bytes'] = sum(row['bytes'] for row in report['deleted'])
        except Exception as error:
            report['result'] = 'error'
            report['error_type'] = type(error).__name__
            report['message'] = 'retention stopped; no further directories removed'
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path('/opt/kxone'))
    parser.add_argument('--go-binary', default='/opt/toolchains/go1.26.10/bin/go')
    parser.add_argument('--keep-days', type=int, default=7)
    parser.add_argument('--keep-verified', type=int, default=5)
    parser.add_argument('--plan', type=Path, help='Require the fingerprint of a preceding dry-run report')
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument('--apply', action='store_true')
    mode.add_argument('--dry-run', action='store_true')
    args = parser.parse_args()
    if args.keep_days < 1 or args.keep_verified < 1:
        parser.error('retention values must be positive')
    if args.plan and not args.apply:
        parser.error('--plan requires --apply')
    try:
        expected = json.loads(args.plan.read_text()) if args.plan else None
        result = run_cleanup(args.root, go_binary=args.go_binary, apply=args.apply, keep_days=args.keep_days, keep_verified=args.keep_verified, expected_plan=expected)
    except Exception as error:
        print(json.dumps({'result': 'error', 'error_type': type(error).__name__, 'message': 'retention stopped; inspect release state'}))
        return 1
    print(json.dumps(result))
    return 0 if result['result'] in ['ok', 'skipped_lock'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
