"""Release-retention behavior using real files and real binary verification."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import fcntl
from datetime import datetime, timezone

import cleanup_releases


class ReleaseRetentionTest(unittest.TestCase):
    now = datetime(2026, 10, 28, 2, tzinfo=timezone.utc)

    @classmethod
    def setUpClass(cls):
        cls.go = shutil.which('go')
        if not cls.go:
            raise RuntimeError('Go required to verify the real release fixture')
        cls.build = tempfile.TemporaryDirectory()
        source = Path(cls.build.name) / 'main.go'
        source.write_text('package main\nimport _ "github.com/mattn/go-sqlite3"\nfunc main(){}\n')
        cls.binary = Path(cls.build.name) / 'fixture'
        subprocess.run([cls.go, 'build', '-o', str(cls.binary), str(source)], cwd=Path(__file__).parent.parent, check=True, capture_output=True)

    @classmethod
    def tearDownClass(cls):
        cls.build.cleanup()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = (Path(self.temp.name) / 'crm').resolve()
        (self.root / 'releases').mkdir(parents=True)
        self.proc = (Path(self.temp.name) / 'proc').resolve()
        self.proc.mkdir()
        (self.proc / 'self').mkdir()
        (self.proc / 'self/mountinfo').write_text('')
        self.current = self.release('20261026T025856Z', secure=True)
        (self.root / 'current').symlink_to(self.current)

    def release(self, name, secure=False):
        target = self.root / 'releases' / name
        for child in ['web', 'src', 'bin', 'evidence']:
            (target / child).mkdir(parents=True)
        (target / 'web/index.html').write_text('synthetic page')
        if secure:
            for binary in ['kxone-server', 'kxone-worker']:
                shutil.copyfile(self.binary, target / 'bin' / binary)
            (target / 'SECURITY_BASELINE').write_text('verified\n')
            (target / 'SESSION_SECURITY_BASELINE').write_text('revocable-sid-v1\n')
        os.utime(target, (self.now.timestamp() - 40 * 86400,) * 2)
        return target

    def run_cleanup(self, **kwargs):
        return cleanup_releases.run_cleanup(self.root, go_binary=self.go, now=self.now, proc_root=self.proc, **kwargs)

    def test_default_is_preview_and_apply_is_idempotent(self):
        old = self.release('20260805023500')
        preview = self.run_cleanup()
        self.assertEqual(preview['mode'], 'dry-run')
        self.assertEqual([row['name'] for row in preview['candidates']], [old.name])
        self.assertTrue(old.exists())
        applied = self.run_cleanup(apply=True)
        self.assertEqual([row['name'] for row in applied['deleted']], [old.name])
        self.assertFalse(old.exists())
        self.assertTrue(self.current.exists())
        self.assertEqual(self.run_cleanup(apply=True)['deleted'], [])

    def test_keeps_five_verified_primaries_and_their_ui_companions(self):
        primaries = [self.release('2026090%dT000000Z' % day, secure=True) for day in range(1, 7)]
        main = self.release('20260907T000000Z-ui.abc', secure=True)
        rollback = self.release(main.name + '-rollback', secure=True)
        self.run_cleanup(apply=True)
        self.assertTrue(main.exists())
        self.assertTrue(rollback.exists())
        self.assertTrue(all(p.exists() for p in primaries[-3:]))
        self.assertTrue(all(not p.exists() for p in primaries[:3]))

    def test_pins_baseline_process_and_current_direct_frontend_dependencies(self):
        baseline = self.release('20260901T000000Z')
        running = self.release('20260902T000000Z')
        previous = self.release('20260903T000000Z')
        rollback = self.release('20260904T000000Z')
        (self.root / 'security-baseline').symlink_to(baseline)
        pid = self.proc / '123'
        pid.mkdir()
        (pid / 'cwd').symlink_to(running / 'src')
        (self.current / 'evidence/frontend-release.txt').write_text('frontend_only=true\nprevious=' + str(previous) + '\nrollback=' + str(rollback) + '\n')
        self.run_cleanup(apply=True)
        self.assertTrue(all(p.exists() for p in [baseline, running, previous, rollback]))

    def test_recent_name_or_mtime_keeps_release_and_unknown_is_skipped(self):
        recent = self.release('20261025T000000Z')
        touched = self.release('20260805023500')
        os.utime(touched, (self.now.timestamp(),) * 2)
        unknown = self.release('20261008T094518Z-sqlite-baseline')
        self.run_cleanup(apply=True)
        self.assertTrue(all(p.exists() for p in [recent, touched, unknown]))

    def test_deployment_lock_skips_without_deleting(self):
        old = self.release('20260805023500')
        with (self.root / 'deploy.lock').open('a') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.assertEqual(self.run_cleanup(apply=True)['result'], 'skipped_lock')
        self.assertTrue(old.exists())

    def test_unknown_session_baseline_is_kept_even_after_five_newer_versions(self):
        unknown = self.release('20260901T000000Z', secure=True)
        (unknown / 'SESSION_SECURITY_BASELINE').write_text('future-baseline')
        for day in range(2, 7):
            self.release('2026090%dT000000Z' % day, secure=True)
        self.run_cleanup(apply=True)
        self.assertTrue(unknown.exists())

    def test_bad_live_metadata_stops_all_deletion(self):
        old = self.release('20260805023500')
        (self.current / 'evidence/frontend-release.txt').write_text('previous=/outside/releases/unknown\n')
        result = self.run_cleanup(apply=True)
        self.assertEqual(result['result'], 'error')
        self.assertTrue(old.exists())

    def test_stale_preview_rejects_a_replaced_directory(self):
        old = self.release('20260805023500')
        preview = self.run_cleanup()
        shutil.rmtree(old)
        self.release(old.name)
        self.assertEqual(self.run_cleanup(apply=True, expected_plan=preview)['result'], 'stale_plan')
        self.assertTrue(old.exists())

    def test_forged_security_marker_cannot_hide_invalid_binaries(self):
        forged = self.release('20260901T000000Z', secure=True)
        (forged / 'bin/kxone-server').write_text('not a Go executable')
        old = self.release('20260805023500')
        self.assertEqual(self.run_cleanup(apply=True)['result'], 'error')
        self.assertTrue(old.exists())
        self.assertTrue(forged.exists())

    def test_symlink_and_nested_mount_are_not_deleted(self):
        outside = Path(self.temp.name) / 'outside'
        outside.mkdir()
        (outside / 'preserved').write_text('preserved')
        link = self.root / 'releases/20260805023500'
        link.symlink_to(outside, target_is_directory=True)
        mounted = self.release('20260901T000000Z')
        nested = mounted / 'web/mount'
        nested.mkdir()
        actual = os.path.ismount
        with patch.object(cleanup_releases.os.path, 'ismount', side_effect=lambda p: Path(p).resolve() == nested.resolve() or actual(p)):
            self.run_cleanup(apply=True)
        self.assertTrue(link.is_symlink())
        self.assertTrue(mounted.exists())
        self.assertEqual((outside / 'preserved').read_text(), 'preserved')

    def test_reference_change_immediately_before_removal_aborts(self):
        old = self.release('20260805023500')
        process = self.proc / '123'
        process.mkdir()
        (process / 'cwd').symlink_to(self.current / 'src')
        actual = os.readlink
        visits = []
        def readlink(path, *args, **kwargs):
            if Path(path) == process / 'cwd':
                visits.append(path)
                if len(visits) == 2:
                    (self.root / 'current').unlink()
                    (self.root / 'current').symlink_to(old)
            return actual(path, *args, **kwargs)
        with patch.object(cleanup_releases.os, 'readlink', side_effect=readlink):
            result = self.run_cleanup(apply=True)
        self.assertEqual(result['result'], 'error')
        self.assertTrue(old.exists())

    def test_live_dependency_does_not_retain_an_unbounded_previous_chain(self):
        direct = self.release('20260902T000000Z')
        ancestor = self.release('20260901T000000Z')
        (self.current / 'evidence/frontend-release.txt').write_text('previous=' + str(direct) + '\n')
        (direct / 'evidence/frontend-release.txt').write_text('previous=' + str(ancestor) + '\n')
        self.run_cleanup(apply=True)
        self.assertTrue(direct.exists())
        self.assertFalse(ancestor.exists())

    def test_boundary_recent_mtime_and_unrelated_data_are_preserved(self):
        boundary = self.release('20261021T020000Z')
        recent = self.release('20261021T020001Z')
        for name in ['uploads', 'backups', 'runtime']:
            (self.root / name).mkdir()
            (self.root / name / 'preserved').write_text(name)
        self.run_cleanup(apply=True)
        self.assertFalse(boundary.exists())
        self.assertTrue(recent.exists())
        for name in ['uploads', 'backups', 'runtime']:
            self.assertEqual((self.root / name / 'preserved').read_text(), name)

    def test_same_device_bind_mount_is_skipped_via_mountinfo(self):
        old = self.release('20260805023500')
        mount = old / 'web/shared data'
        mount.mkdir()
        (mount / 'preserved').write_text('real runtime data')
        self.assertEqual(mount.stat().st_dev, old.stat().st_dev)
        self.assertFalse(os.path.ismount(mount))
        escaped = str(mount).replace(' ', '\\040')
        (self.proc / 'self/mountinfo').write_text('12 1 0:42 / ' + escaped + ' rw - ext4 /dev/example rw\n')
        self.run_cleanup(apply=True)
        self.assertTrue(old.exists())
        self.assertEqual((mount / 'preserved').read_text(), 'real runtime data')

    def test_unknown_named_live_version_still_protects_direct_dependencies(self):
        active = self.release('custom-front', secure=True)
        previous = self.release('20260902T000000Z')
        rollback = self.release('20260903T000000Z')
        (self.root / 'current').unlink()
        (self.root / 'current').symlink_to(active)
        (active / 'evidence/frontend-release.txt').write_text('previous=' + str(previous) + '\nrollback=' + str(rollback) + '\n')
        result = self.run_cleanup(apply=True)
        self.assertEqual(result['result'], 'ok')
        self.assertTrue(previous.exists())
        self.assertTrue(rollback.exists())

    def test_dangling_live_metadata_link_blocks_all_deletion(self):
        old = self.release('20260805023500')
        (self.current / 'evidence/frontend-release.txt').symlink_to(self.current / 'missing-metadata')
        self.assertEqual(self.run_cleanup(apply=True)['result'], 'error')
        self.assertTrue(old.exists())


if __name__ == '__main__':
    unittest.main()
